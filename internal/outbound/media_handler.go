// POST /v1/media (upload) and GET /v1/media/{id} (download).
//
// WHY THE UPLOAD BELONGS TO THE GATEWAY: without it, whoever has the bytes
// needs to host a public URL just for Meta to fetch — which is what a
// system on this network had to build after a send FAILED SILENTLY when it
// didn't fetch. With the upload here, no consumer hosts anything, and
// base64 in the /v1/messages body is rejected with a named error that
// points here (ErrMediaBase64).
//
// WHY THE DOWNLOAD RETURNS TWO MIMES: Meta reports the SAME media as
// `audio/ogg; codecs=opus` in the message payload and as `audio/ogg` on
// `GET /{media_id}`. It's the `; codecs=opus` that makes WhatsApp render a
// PLAYABLE VOICE NOTE; resending with the other one delivers a FILE
// ATTACHMENT — and the message arrives, with no error at all. Cost paid in
// production on this network on 2026-07-20 (docs/ARMADILHAS.md). That's why
// both travel, NAMED, and the gateway does not choose or normalize either
// one: the choice belongs to the consumer, and they can only make it if
// they receive both.
//
// THE PAYLOAD MIME COMES FROM THE CONSUMER (`?mime_do_payload=`), not from a
// record of ours, because the gateway DOES NOT STORE THE MESSAGE — the line
// from §2 of the spec. Whoever received the event has that mime; the
// gateway just returns it alongside the other one, so the comparison
// happens in one place.
//
// THE ORDER OF THE GUARDS is the same as for sending, and each one exists
// because the previous one isn't enough:
//
//	authenticate -> check the bond with the instance -> instance active? ->
//	validate mime and cap PER CATEGORY -> only then touch the wire
//
// THE BYTES CROSS IN STREAMING: they don't go to disk and don't go to the
// log. Media is content, and this project's line is "configuration yes,
// message never".
package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// mediaDeadline is OURS, and is deliberately not the instance's TimeoutMs:
// that deadline was sized for a small JSON POST (2s on a test instance),
// and applying it to an upload of dozens of MB would kill legitimate
// transfers midway. This cap exists only so that a connection that hangs
// doesn't hold the goroutine forever; the deadline that governs the normal
// case is the consumer's own, since they're on the other end feeding the
// bytes.
const mediaDeadline = 5 * time.Minute

// The two headers that carry the mime pair. Explicit names on purpose:
// `Content-Type` would be THE gateway choosing, and a consumer that read
// only that one would end up with the wrong choice without ever seeing the
// difference.
const (
	payloadMimeHeader = "X-Zapgw-Mime-Do-Payload"
	getMimeHeader     = "X-Zapgw-Mime-Do-Get"
)

// partName is the name of the multipart field that carries the bytes.
const partName = "arquivo"

type MediaHandler struct {
	store  *config.Store
	auth   *Authenticator
	client *meta.Client
	// throttleLog suppresses repeated VALIDATION-rejection logs (T-037) —
	// see logThrottle and logRejection in handler.go.
	throttleLog *logThrottle
	// types declares which instance types this route serves (T-111) — see
	// the comment on AcceptedTypes in types.go.
	types AcceptedTypes
}

// NewMediaHandler builds the two routes. Like the health probe, it holds
// NO MEDIA state: no byte stays in the process, and that's how it's proven
// by construction.
//
// `types` is WhatsAppOnly: upload() and download() call meta.Client with
// inst.PhoneNumberID, a field that only exists on config.TypeWhatsApp —
// empty on any Instagram instance (T-111). There is no Instagram
// equivalent in this slice.
func NewMediaHandler(store *config.Store, auth *Authenticator, client *meta.Client, types AcceptedTypes) http.Handler {
	h := &MediaHandler{
		store: store, auth: auth, client: client, throttleLog: newLogThrottle(logSuppressionWindow),
		types: types,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/media", h.upload)
	mux.HandleFunc("GET /v1/media/{id}", h.download)
	return mux
}

// instanceAuthorized runs the three guards that apply to BOTH routes.
// Returns the authenticated consumer and the ready instance, or false — in
// which case the response has already been written.
//
// A single function, not a copy in each handler: the asymmetry between two
// places that solve the same problem IS the bug (docs/ARMADILHAS.md).
//
// `rota` is only for the rejection log (T-037) to distinguish upload from
// download — both call this function.
func (h *MediaHandler) instanceAuthorized(w http.ResponseWriter, r *http.Request, route string) (config.Consumer, config.Instance, bool) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
			respondError(w, http.StatusUnauthorized, "config", "token ausente ou invalido", 0)
			return config.Consumer{}, config.Instance{}, false
		}
		log.Printf("zapgw: erro de store ao autenticar em midia: %v", err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
		return config.Consumer{}, config.Instance{}, false
	}

	slug := strings.TrimSpace(r.URL.Query().Get("instancia"))
	// Before any byte and before any call to Meta: without this, a token
	// leaked from system A would spend system B's instance quota — and
	// would discover which slugs exist from the response status.
	if !CanUse(consumer, slug) {
		// 403 YES (T-037): whoever got here already authenticated, and this
		// is someone's config error — which is worth investigating.
		logRejection(h.throttleLog, route, slug, consumer.Name, "instancia nao autorizada para este consumidor")
		respondError(w, http.StatusForbidden, "config",
			"instancia nao autorizada para este consumidor (mande ?instancia={slug})", 0)
		return config.Consumer{}, config.Instance{}, false
	}

	inst, err := h.store.FindInstance(slug)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			logRejection(h.throttleLog, route, slug, consumer.Name, "instancia desconhecida")
			respondError(w, http.StatusNotFound, "config", "instancia desconhecida", 0)
			return config.Consumer{}, config.Instance{}, false
		}
		log.Printf("zapgw: erro de store ao buscar instancia %q em midia: %v", slug, err)
		respondError(w, http.StatusServiceUnavailable, "retentavel", "indisponivel", 0)
		return config.Consumer{}, config.Instance{}, false
	}
	if !inst.Active {
		respondError(w, http.StatusServiceUnavailable, "retentavel", "instancia pausada", 0)
		return config.Consumer{}, config.Instance{}, false
	}
	// T-111: AFTER the bond check (403) and existence (404) — NEVER before,
	// otherwise this route becomes an oracle for "what type is this slug"
	// for someone who doesn't own it. checkType already writes the
	// 400/config response when it rejects.
	if !checkType(w, h.types, inst, "") {
		return config.Consumer{}, config.Instance{}, false
	}
	return consumer, inst, true
}

func (h *MediaHandler) upload(w http.ResponseWriter, r *http.Request) {
	consumer, inst, ok := h.instanceAuthorized(w, r, "POST /v1/media")
	if !ok {
		return
	}

	// MultipartReader, never ParseMultipartForm: the latter writes the file
	// to DISK above a threshold. Media is client content, and content does
	// not stop on disk in this project — not for an instant, not even in
	// /tmp.
	parts, err := r.MultipartReader()
	if err != nil {
		logRejection(h.throttleLog, "POST /v1/media", inst.Slug, consumer.Name,
			"o corpo precisa ser multipart/form-data com uma parte chamada "+partName)
		respondError(w, http.StatusBadRequest, "permanente",
			"o corpo precisa ser multipart/form-data com uma parte chamada "+partName, 0)
		return
	}

	part, err := filePart(parts)
	if err != nil {
		logRejection(h.throttleLog, "POST /v1/media", inst.Slug, consumer.Name,
			"nao veio a parte "+partName+" com os bytes")
		respondError(w, http.StatusBadRequest, "permanente",
			"nao veio a parte "+partName+" com os bytes", 0)
		return
	}
	defer part.Close()

	// The mime comes from the PART HEADER and goes to the wire EXACTLY AS
	// IT ARRIVED. It's only parsed to figure out the category
	// (meta.CategoryOfMime reads the base type); trimming the parameter to
	// "normalize" it would destroy the `; codecs=opus`, which is what makes
	// the voice note exist.
	mimeType := part.Header.Get("Content-Type")
	category, err := meta.CategoryOfMime(mimeType)
	if err != nil {
		// 415 is the exact rejection, and it happens BEFORE the wire:
		// sending and hoping costs bandwidth on both sides, and Meta's
		// verdict would only arrive after the whole upload.
		//
		// The log message does NOT carry the declared mimeType: it came
		// from the consumer, and the package only names the known
		// category, never the rejected value (T-037).
		logRejection(h.throttleLog, "POST /v1/media", inst.Slug, consumer.Name, "mime nao aceito pelo gateway")
		respondError(w, http.StatusUnsupportedMediaType, "permanente",
			"mime nao aceito pelo gateway (declare o Content-Type da parte "+partName+
				"; a lista e do gateway, nao da Meta)", 0)
		return
	}

	ceiling := meta.CategoryCap(category)
	// If the consumer declared the size, the rejection happens WITHOUT
	// OPENING any connection at all. The multipart body is always >= the
	// file, so a body above the cap is already sufficient proof to reject
	// here.
	if r.ContentLength > ceiling {
		logRejection(h.throttleLog, "POST /v1/media", inst.Slug, consumer.Name,
			"acima do teto do gateway para a categoria "+string(category))
		respondCapError(w, category, ceiling)
		return
	}

	// And if they did NOT declare it (chunked), the cap still applies,
	// enforced mid-stream — otherwise the guard would only protect whoever
	// declares the size, which is exactly who doesn't need it.
	limited := &cappedReader{r: part, remaining: ceiling}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), mediaDeadline)
	defer cancel()

	id, err := h.client.UploadMedia(ctx, inst.PhoneNumberID, inst.SendToken,
		mimeType, part.FileName(), limited)
	if err != nil {
		// A blown cap arrives here as a transport failure (the upload pipe
		// was closed with an error), and calling that "Meta didn't
		// respond" would send the consumer to retry forever. The reader
		// itself is the one that knows what happened.
		if limited.overflowed.Load() {
			logRejection(h.throttleLog, "POST /v1/media", inst.Slug, consumer.Name,
				"acima do teto do gateway para a categoria "+string(category)+" (estourou no meio do stream)")
			respondCapError(w, category, ceiling)
			return
		}
		// We do NOT interpolate the raw error: it may carry the Graph API
		// URL with the client's phone_number_id.
		respondMediaError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// Field name matches what /v1/messages expects to get back: whoever
	// uploads it sends it, with no translation in between.
	_ = json.NewEncoder(w).Encode(map[string]string{"media_id": id})
}

// filePart looks for the `file` part, skipping small text fields
// the consumer's HTTP client might have placed before it. The discard is
// LIMITED: a giant text field can't become a memory-consumption path that
// isn't even media.
func filePart(parts *multipart.Reader) (*multipart.Part, error) {
	for {
		part, err := parts.NextPart()
		if err != nil {
			return nil, err
		}
		if part.FormName() == partName {
			return part, nil
		}
		if _, err := io.Copy(io.Discard, io.LimitReader(part, 4<<10)); err != nil {
			part.Close()
			return nil, err
		}
		part.Close()
	}
}

func respondCapError(w http.ResponseWriter, category meta.Category, ceiling int64) {
	respondError(w, http.StatusRequestEntityTooLarge, "permanente",
		"acima do teto do gateway para a categoria "+string(category)+
			" ("+bytesAsText(ceiling)+"); o teto e NOSSO, nao da Meta", 0)
}

func (h *MediaHandler) download(w http.ResponseWriter, r *http.Request) {
	// The payload mime is CHECKED before any call: it becomes a header
	// value, and a header value with CR/LF is injection. The check does
	// NOT replace the value — what goes up is the original, otherwise
	// "checking" would turn into normalizing through the back door, which
	// is exactly what this endpoint exists to not do.
	payloadMime := r.URL.Query().Get("mime_do_payload")
	if payloadMime != "" && !wellFormedMime(payloadMime) {
		respondError(w, http.StatusBadRequest, "permanente",
			"mime_do_payload nao e um mime valido; mande o valor de midia_mime_payload "+
				"exatamente como veio no evento", 0)
		return
	}

	_, inst, ok := h.instanceAuthorized(w, r, "GET /v1/media/{id}")
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), mediaDeadline)
	defer cancel()

	description, err := h.client.DescribeMedia(ctx, r.PathValue("id"), inst.SendToken)
	if err != nil {
		respondMediaError(w, err)
		return
	}
	body, err := h.client.OpenMedia(ctx, description, inst.SendToken)
	if err != nil {
		respondMediaError(w, err)
		return
	}
	defer body.Close()

	// BOTH MIMES, NAMED AND RAW. If the consumer didn't send the payload
	// one, the header stays ABSENT: copying the GET one into its place
	// would be inventing data, and the consumer would resend it thinking
	// they have the right mime.
	if payloadMime != "" {
		w.Header().Set(payloadMimeHeader, payloadMime)
	}
	w.Header().Set(getMimeHeader, description.MimeFromGet)
	// Content-Type deliberately NEUTRAL: putting one of the two mimes here
	// would be the gateway choosing, and whoever read only Content-Type
	// would end up with the wrong choice without ever seeing there were
	// two.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)

	// Direct streaming: the bytes don't stop in a buffer of ours, don't go
	// to disk and don't go to the log.
	if _, err := io.Copy(w, body); err != nil {
		// Header and status were already written; there's no error
		// response left to give. The line carries neither the bytes nor
		// the token — just the fact.
		log.Printf("zapgw: download de midia interrompido no meio (instancia=%q)", inst.Slug)
	}
}

// respondMediaError uses the SAME taxonomy as sending (handler.go):
// whoever consumes this gateway shouldn't have to learn two. `permanente`
// becomes 400, `retentavel` 503, and whatever didn't come from Meta becomes
// 502 `desconhecido`.
func respondMediaError(w http.ResponseWriter, err error) {
	if errors.Is(err, meta.ErrInvalidPhoneNumberID) || errors.Is(err, meta.ErrInvalidMediaID) {
		respondError(w, http.StatusBadRequest, string(meta.ClassPermanent),
			"identificador com forma invalida; o pedido nem chegou a sair do gateway", 0)
		return
	}
	if errors.Is(err, meta.ErrBadMediaURL) {
		// NEEDS A HUMAN only if it repeats: Meta returned a description
		// with no usable url (or with http://, which would deliver the
		// token in the clear). There's nothing for the consumer to fix.
		respondError(w, http.StatusBadGateway, string(meta.ClassUnknown),
			"a Meta nao devolveu um endereco utilizavel para esta midia", 0)
		return
	}
	var me *meta.MetaError
	if errors.As(err, &me) {
		status := http.StatusBadGateway
		switch me.Class {
		case meta.ClassRetryable:
			status = http.StatusServiceUnavailable
		case meta.ClassPermanent:
			status = http.StatusBadRequest
		}
		respondError(w, status, string(me.Class), me.Message, me.MetaCode)
		return
	}
	if errors.Is(err, meta.ErrUploadWithoutID) {
		respondError(w, http.StatusBadGateway, string(meta.ClassUnknown),
			"a Meta respondeu sem media_id; a midia pode ter subido — confira antes de repetir", 0)
		return
	}
	respondError(w, http.StatusBadGateway, string(meta.ClassUnknown),
		"falha ao falar com a Meta", 0)
}

// cappedReader cuts the stream off at the category cap.
//
// `estourou` is atomic because the READER is the goroutine writing the
// multipart (inside meta.UploadMedia) and the one that CHECKS it is the
// handler's goroutine, after the call returns. A raw counter shared between
// two goroutines already cost this project a Critical (docs/ARMADILHAS.md,
// "Go / concorrência").
type cappedReader struct {
	r          io.Reader
	remaining  int64
	overflowed atomic.Bool
}

var errAboveCap = errors.New("outbound: midia acima do teto da categoria")

func (l *cappedReader) Read(p []byte) (int, error) {
	if l.remaining < 0 {
		l.overflowed.Store(true)
		return 0, errAboveCap
	}
	// Reads at most teto+1: that's how "fits exactly" is told apart from
	// "went over" without allocating the whole body of whoever sent too
	// much.
	if int64(len(p)) > l.remaining+1 {
		p = p[:l.remaining+1]
	}
	n, err := l.r.Read(p)
	l.remaining -= int64(n)
	if l.remaining < 0 {
		l.overflowed.Store(true)
		return n, errAboveCap
	}
	return n, err
}

// wellFormedMime checks that the value can become a header AND that it
// describes media.
//
// `mime.ParseMediaType` alone is NOT enough, and the test that proved it:
// it is deliberately lenient (it accepts a bare token, because it also
// serves `Content-Disposition: form-data`), so "not-a-mime" would pass
// through and become a header the consumer would read as a real mime. The
// requirement of a `type/subtype` is what separates "checked" from
// "accepted by mistake".
//
// The checked value is DISCARDED on purpose: what goes into the header is
// the ORIGINAL string. ParseMediaType returns the type lowercased and
// rewrites the parameters — using its result would be normalizing through
// the back door, exactly what this endpoint exists to not do.
func wellFormedMime(value string) bool {
	base, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	kind, subtype, hasSlash := strings.Cut(base, "/")
	return hasSlash && kind != "" && subtype != ""
}

// bytesAsText writes the cap in MiB/KiB so the error message is
// actionable — "acima de 524288" doesn't tell anyone what to do.
func bytesAsText(n int64) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatInt(n/(1<<20), 10) + " MiB"
	case n >= 1<<10:
		return strconv.FormatInt(n/(1<<10), 10) + " KiB"
	default:
		return strconv.FormatInt(n, 10) + " bytes"
	}
}
