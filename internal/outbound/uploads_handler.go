// POST /v1/uploads — the header_handle a WhatsApp template needs to declare
// a HEADER of format IMAGE/VIDEO/DOCUMENT (T-241).
//
// WHY THIS ROUTE EXISTS, AND WHY IT ISN'T /v1/media: `POST /v1/templates`
// passes `componentes` through to Meta AS THE CONSUMER SENT IT (T-153's
// sibling design), and a HEADER example needs `example.header_handle` — a
// value that only comes out of Meta's Resumable Upload API. The media_id
// from POST /v1/media does NOT work here; without this route, the consumer
// has no way to obtain that handle at all, because the gateway's rule is
// the same as media's: the consumer never talks to the Graph API directly.
//
// THE ORDER OF THE GUARDS is media's, unchanged, and instanceAuthorized
// (media_handler.go, T-241 extracted it to a package function precisely so
// this route could reuse it instead of copying the block):
//
//	authenticate -> bond with the instance (403) -> instance exists (404) ->
//	active (503) -> checkType -> mime accepted -> ceiling -> only then the wire
//
// THE BYTES CROSS IN STREAMING, same discipline as media.go: `r.Body` goes
// straight into meta.Client.UploadHandle, never buffered here and never
// written to disk.
//
// `Content-Length` IS MANDATORY HERE, unlike media's upload: it becomes the
// Resumable Upload API's own required `file_length` parameter, so there is
// no chunked-body case to guard against mid-stream — the declared value is
// checked before any connection opens, and that's the whole guard.
package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"mime"
	"net/http"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

// acceptedUploadMimes is the GATEWAY'S OWN, narrower subset of what
// /v1/media accepts: the Resumable Upload doc lists exactly these four
// (developers.facebook.com/docs/graph-api/guides/upload, read 2026-09-15).
// `image/jpg` is deliberately absent — /v1/media doesn't accept it either
// (media_handler.go), and this route holds the same line. The value is the
// default filename EXTENSION used when the consumer omits `?file_name=`.
var acceptedUploadMimes = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"video/mp4":       ".mp4",
	"application/pdf": ".pdf",
}

type UploadsHandler struct {
	store  *config.Store
	auth   *Authenticator
	client *meta.Client
	// throttleLog suppresses repeated VALIDATION-rejection logs (T-037) —
	// see logThrottle and logRejection in handler.go.
	throttleLog *logThrottle
	// types declares which instance types this route serves (T-111) — see
	// the comment on AcceptedTypes in types.go.
	types AcceptedTypes
	// counter is the old-name migration metric (T-208,
	// config.CounterOldNameUsed). This route has ONE ENTRADA-QUERY
	// parameter pair, `instancia`/`instance`, shared with media through
	// instanceAuthorized — POSITIONAL AND MANDATORY, same discipline as
	// MediaHandler.counter.
	counter *config.Counter
}

// NewUploadsHandler builds the route. Like MediaHandler, it holds NO
// upload state: no byte stays in the process.
//
// `types` is WhatsAppOnly: the handle this route returns is only useful
// for a WhatsApp template's HEADER example, and inst.SendToken (used for
// both the app-id discovery and the upload calls) is the same credential
// media uses. There is no Instagram equivalent in this slice.
func NewUploadsHandler(store *config.Store, auth *Authenticator, client *meta.Client, counter *config.Counter, types AcceptedTypes) http.Handler {
	h := &UploadsHandler{
		store: store, auth: auth, client: client, throttleLog: newLogThrottle(logSuppressionWindow),
		counter: counter, types: types,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/uploads", h.upload)
	return mux
}

func (h *UploadsHandler) upload(w http.ResponseWriter, r *http.Request) {
	consumer, inst, ok, oldInstanceParam := instanceAuthorized(w, r, "POST /v1/uploads", h.store, h.auth, h.throttleLog, h.types)
	if !ok {
		return
	}
	if oldInstanceParam {
		h.counter.Record(inst.Slug, config.CounterOldNameUsed)
	}

	// `Content-Length` IS the Resumable Upload API's `file_length`, and
	// the API requires it up front (before any byte crosses) — so its
	// absence is rejected the same way, before any byte crosses here.
	// `r.Header.Get`, not just `r.ContentLength < 0`: a request with
	// NEITHER a Content-Length header NOR chunked encoding reads back as
	// ContentLength == 0 in net/http, indistinguishable from a genuinely
	// declared empty body — checking the header text is what tells the
	// two apart.
	if r.Header.Get("Content-Length") == "" || r.ContentLength < 0 {
		logRejection(h.throttleLog, "POST /v1/uploads", inst.Slug, consumer.Name,
			"Content-Length obrigatorio (esta rota exige o tamanho declarado)")
		respondError(w, http.StatusLengthRequired, "permanent",
			"Content-Length obrigatorio; a Resumable Upload API da Meta exige o tamanho declarado do arquivo", 0)
		return
	}

	// The mime comes from the REQUEST'S Content-Type — the body here is
	// raw bytes, not multipart, so there's no per-part header the way
	// media's upload has one.
	mimeType := r.Header.Get("Content-Type")
	base, _, err := mime.ParseMediaType(mimeType)
	extension, accepted := "", false
	if err == nil {
		extension, accepted = acceptedUploadMimes[base]
	}
	if !accepted {
		logRejection(h.throttleLog, "POST /v1/uploads", inst.Slug, consumer.Name, "mime nao aceito pelo gateway")
		respondError(w, http.StatusUnsupportedMediaType, "permanent",
			"mime nao aceito pelo gateway (a lista e do gateway, nao da Meta: image/jpeg, image/png, video/mp4, application/pdf)", 0)
		return
	}

	// The category and its ceiling come from the SAME table /v1/media
	// reads (meta.CategoryOfMime / meta.CategoryCap) — two tables for the
	// same question is this project's mother trap. Success here is
	// guaranteed: every mime in acceptedUploadMimes is a subset of a
	// category CategoryOfMime already knows.
	category, err := meta.CategoryOfMime(mimeType)
	if err != nil {
		// Unreachable in practice (see the comment above), but a route
		// that trusted that silently would be exactly the kind of
		// assumption this project's traps document — fail closed instead.
		logRejection(h.throttleLog, "POST /v1/uploads", inst.Slug, consumer.Name, "mime nao aceito pelo gateway")
		respondError(w, http.StatusUnsupportedMediaType, "permanent",
			"mime nao aceito pelo gateway (a lista e do gateway, nao da Meta: image/jpeg, image/png, video/mp4, application/pdf)", 0)
		return
	}
	ceiling := meta.CategoryCap(category)
	if r.ContentLength > ceiling {
		logRejection(h.throttleLog, "POST /v1/uploads", inst.Slug, consumer.Name,
			"acima do teto do gateway para a categoria "+string(category))
		respondCapError(w, category, ceiling)
		return
	}

	fileName := r.URL.Query().Get("file_name")
	if fileName == "" {
		// The Meta API requires the field with no observable effect on
		// this route's outcome — the default exists only to satisfy it.
		fileName = "header" + extension
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), mediaDeadline)
	defer cancel()

	// 🔴 Three separate calls, in sequence, EACH ONE NAMED (T-241 addendum):
	// the consumer needs to tell "app id discovery failed" apart from
	// "session creation failed" apart from "the bytes were rejected"
	// WITHOUT depending on Meta's own error prose, which is free to
	// change. This is also the ONE part of this design that was never
	// exercised against the real Meta (T-241's Verify note) — the app id
	// is discovered from the token EVERY call; see meta.Client.AppID's
	// comment for why the instance doesn't store one.
	appID, err := h.client.AppID(ctx, inst.SendToken)
	if err != nil {
		respondUploadStepError(w, "app_id", "descobrir o App ID (GET /app)", err)
		return
	}

	sessionID, err := h.client.CreateUploadSession(ctx, appID, inst.SendToken, mimeType, fileName, r.ContentLength)
	if err != nil {
		respondUploadStepError(w, "session", "criar a sessao de upload (POST /{app-id}/uploads)", err)
		return
	}

	handle, err := h.client.CompleteUpload(ctx, sessionID, inst.SendToken, r.ContentLength, r.Body)
	if err != nil {
		respondUploadStepError(w, "upload", "enviar os bytes (POST /upload:...)", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// Field name matches what a template's `example.header_handle`
	// expects to receive: whoever calls this route puts it there directly.
	_ = json.NewEncoder(w).Encode(map[string]string{"handle": handle})
}

// respondUploadStepError answers a failure from ANY of this route's three
// Meta calls, tagging the body with WHICH step failed — `step`: "app_id",
// "session", or "upload" (T-241 addendum). The consumer asked for this
// AS A MACHINE-READABLE FIELD, not just prose: `label` still gets
// prepended to Message for a human reading the body or the log, but
// `errorResponse.Error.Step` is what code should switch on.
//
// It duplicates respondMediaError's taxonomy (media_handler.go) rather
// than calling it, because respondMediaError has no way to carry the
// extra field without every OTHER route's call site learning a parameter
// it never needs — errorResponse itself (handler.go) is the single source
// for the wire shape; only the taxonomy that fills it is repeated here,
// and ONLY for this route.
func respondUploadStepError(w http.ResponseWriter, step, label string, err error) {
	var resp errorResponse
	resp.Error.Step = step
	status := http.StatusBadGateway

	switch {
	case errors.Is(err, meta.ErrAppIDWithoutID):
		resp.Error.Class = string(meta.ClassUnknown)
		resp.Error.Message = label + ": a Meta respondeu com sucesso mas sem o app id"
	case errors.Is(err, meta.ErrUploadSessionWithoutID):
		resp.Error.Class = string(meta.ClassUnknown)
		resp.Error.Message = label + ": a Meta respondeu com sucesso mas sem o id da sessao"
	case errors.Is(err, meta.ErrUploadWithoutHandle):
		resp.Error.Class = string(meta.ClassUnknown)
		resp.Error.Message = label + ": a Meta respondeu com sucesso mas sem o handle"
	case errors.Is(err, meta.ErrUploadSessionIDShape):
		// T-242: this error surfaces from the THIRD call (CompleteUpload),
		// so the call site above tagged it "upload" -- but the thing that
		// is wrong is what the SECOND call (CreateUploadSession) handed
		// back. Overriding Step here, not at the call site, keeps that
		// call site simple and puts the correction exactly where the
		// taxonomy already lives.
		resp.Error.Step = "session"
		resp.Error.Class = string(meta.ClassUnknown)
		resp.Error.Message = "a Meta devolveu um id de sessao de upload em formato inesperado"
	default:
		var me *meta.MetaError
		if errors.As(err, &me) {
			switch me.Class {
			case meta.ClassRetryable:
				status = http.StatusServiceUnavailable
			case meta.ClassPermanent:
				status = http.StatusBadRequest
			}
			resp.Error.Class = string(me.Class)
			resp.Error.Message = label + ": " + me.Message
			resp.Error.MetaCode = me.MetaCode
			resp.Error.MetaDetail = me.Detail
			resp.Error.MetaSubcode = me.Subcode
			resp.Error.MetaExplanation = me.Explanation
			resp.Error.MetaTrace = me.Trace
		} else {
			resp.Error.Class = string(meta.ClassUnknown)
			resp.Error.Message = label + ": falha ao falar com a Meta"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}
