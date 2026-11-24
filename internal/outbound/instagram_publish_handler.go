// POST /v1/instances/{slug}/instagram-posts and
// GET  /v1/instances/{slug}/instagram-publishing-limit (T-270)
//
// WHY IT EXISTS: a consumer that publishes approved posts to the feed keeps
// Meta behind the gateway. The post is a three-call flow at Meta (container,
// status poll, publish — see internal/meta/instagram_publish.go) and only
// the last call makes anything visible.
//
// IDEMPOTENCY: a publish is not retry-safe at Meta — a second media_publish
// is a duplicate post on a public feed, the defect this guards. The route
// uses the SAME reservation POST /v1/messages uses (ReserveIdempotency over a
// canonical request hash). The store keeps ONE string per key
// (`wa_message_id`), so a finished publish stores "<media_id>|<container_id>"
// there: no schema change. The kind is not stored; it is derived from the
// request, which the hash already proves identical.
//
// The key is released when it is KNOWN that nothing was published (any
// failure before the publish call, or a Meta refusal of it) and HELD when
// the publish call itself ended in an unknown outcome (transport error,
// deadline) — the same asymmetry as send(): holding risks a 409 until the
// TTL purge, releasing risks a duplicate post.
//
// GUARD ORDER (the same as every instance route):
//
//	authenticate -> bond with the instance (403) -> instance exists (404)
//	-> instance active (503) -> instance type (400) -> key / body (400) -> Meta
package outbound

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/httpx"
	"github.com/iscarelli/zapgw/internal/meta"
)

const (
	maxPostImages  = 20
	maxCaptionRune = 2200
)

type InstagramPublishHandler struct {
	store         *config.Store
	auth          *Authenticator
	client        *meta.Client
	baseInstagram string
	maxBytes      int
	timings       meta.InstagramPublishTimings
	types         AcceptedTypes
}

// NewInstagramPublishHandler builds the routes against the real Instagram
// host with the production poll timings. `types` is positional and mandatory
// (T-111).
func NewInstagramPublishHandler(store *config.Store, auth *Authenticator, client *meta.Client, maxBytes int, types AcceptedTypes) http.Handler {
	return NewInstagramPublishHandlerWithBase(store, auth, client, meta.DefaultInstagramRenewalBase, maxBytes,
		meta.InstagramPublishTimings{}, types)
}

// NewInstagramPublishHandlerWithBase has the host root and the poll timings
// injectable (a zero timing takes the production default).
func NewInstagramPublishHandlerWithBase(store *config.Store, auth *Authenticator, client *meta.Client, baseInstagram string, maxBytes int, timings meta.InstagramPublishTimings, types AcceptedTypes) http.Handler {
	h := &InstagramPublishHandler{store: store, auth: auth, client: client, baseInstagram: baseInstagram,
		maxBytes: maxBytes, timings: timings, types: types}
	mux := http.NewServeMux()
	// String LITERALS: the isolation table reads these registrations by text.
	mux.HandleFunc("POST /v1/instances/{slug}/instagram-posts", h.publish)
	mux.HandleFunc("GET /v1/instances/{slug}/instagram-publishing-limit", h.limit)
	return mux
}

type instagramPostImage struct {
	URL     string `json:"url"`
	AltText string `json:"alt_text,omitempty"`
}

type instagramPostRequest struct {
	Images  []instagramPostImage `json:"images"`
	Caption string               `json:"caption,omitempty"`
}

type instagramPostResponse struct {
	MediaID     string `json:"media_id"`
	ContainerID string `json:"container_id"`
	Kind        string `json:"kind"`
}

type instagramLimitResponse struct {
	QuotaUsage     int `json:"quota_usage"`
	QuotaTotal     int `json:"quota_total"`
	QuotaDurationS int `json:"quota_duration_s"`
}

// instagramPostHash is the canonical request hash: the slug is part of it
// (the same key on another instance is another request), and so is a route
// marker, so a key reused on POST /v1/messages can never match.
func instagramPostHash(slug string, p instagramPostRequest) string {
	raw, _ := json.Marshal(struct {
		Route    string `json:"route"`
		Instance string `json:"instance"`
		Request  instagramPostRequest
	}{"instagram-posts", slug, p})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// validate trims the fields in place and returns the consumer-facing
// message of the first problem, or "".
func (p *instagramPostRequest) validate() string {
	if len(p.Images) == 0 {
		return "images must have at least 1 item"
	}
	if len(p.Images) > maxPostImages {
		return "images must have at most 20 items"
	}
	for i := range p.Images {
		p.Images[i].URL = strings.TrimSpace(p.Images[i].URL)
		p.Images[i].AltText = strings.TrimSpace(p.Images[i].AltText)
		u, err := url.Parse(p.Images[i].URL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return "every images[].url must be an absolute https:// URL"
		}
	}
	p.Caption = strings.TrimSpace(p.Caption)
	if utf8.RuneCountInString(p.Caption) > maxCaptionRune {
		return "caption must have at most 2200 characters"
	}
	return ""
}

// authorize runs the guards common to both routes and returns the instance,
// or writes the response and returns ok=false.
func (h *InstagramPublishHandler) authorize(w http.ResponseWriter, r *http.Request, route string) (config.Consumer, config.Instance, bool) {
	consumer, err := h.auth.Authenticate(r.Header.Get("Authorization"))
	if err != nil {
		if errors.Is(err, ErrNoToken) || errors.Is(err, ErrInvalidToken) {
			respondError(w, http.StatusUnauthorized, "config", "missing or invalid token", 0)
			return config.Consumer{}, config.Instance{}, false
		}
		log.Printf("zapgw: store error while authenticating on the %s route: %v", route, err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "unavailable", 0)
		return config.Consumer{}, config.Instance{}, false
	}
	slug := r.PathValue("slug")
	if !CanUse(consumer, slug) {
		respondError(w, http.StatusForbidden, "config", "instance not authorized for this consumer", 0)
		return config.Consumer{}, config.Instance{}, false
	}
	inst, err := h.store.FindInstance(slug)
	if err != nil {
		if errors.Is(err, config.ErrInstanceNotFound) {
			respondError(w, http.StatusNotFound, "config", "unknown instance", 0)
			return config.Consumer{}, config.Instance{}, false
		}
		log.Printf("zapgw: store error while looking up instance %q on the %s route: %v", slug, route, err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "unavailable", 0)
		return config.Consumer{}, config.Instance{}, false
	}
	if !inst.Active {
		respondError(w, http.StatusServiceUnavailable, "retryable", "instance paused", 0)
		return config.Consumer{}, config.Instance{}, false
	}
	// AFTER the bond and the existence check, never before — otherwise this
	// route becomes an oracle of "what type is this slug".
	if !checkType(w, h.types, inst, "") {
		return config.Consumer{}, config.Instance{}, false
	}
	return consumer, inst, true
}

func (h *InstagramPublishHandler) publish(w http.ResponseWriter, r *http.Request) {
	consumer, inst, ok := h.authorize(w, r, "instagram-posts")
	if !ok {
		return
	}

	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		respondError(w, http.StatusBadRequest, "permanent", "Idempotency-Key header is required", 0)
		return
	}
	raw, err := httpx.ReadRaw(r.Body, h.maxBytes)
	if err != nil {
		if errors.Is(err, httpx.ErrBodyTooLarge) {
			respondError(w, http.StatusRequestEntityTooLarge, "permanent", "body too large", 0)
			return
		}
		respondError(w, http.StatusBadRequest, "retryable", "body was not read in full", 0)
		return
	}
	var p instagramPostRequest
	if err := json.Unmarshal(raw, &p); err != nil {
		respondError(w, http.StatusBadRequest, "permanent", "body is not valid JSON", 0)
		return
	}
	if msg := p.validate(); msg != "" {
		respondError(w, http.StatusBadRequest, "permanent", msg, 0)
		return
	}
	kind := "image"
	if len(p.Images) > 1 {
		kind = "carousel"
	}

	stored, reserved, err := h.store.ReserveIdempotency(consumer.Name, key, instagramPostHash(inst.Slug, p))
	if err != nil {
		if errors.Is(err, config.ErrKeyWithDifferentRequest) {
			respondError(w, http.StatusUnprocessableEntity, "permanent",
				"this idempotency key was already used for a different request", 0)
			return
		}
		log.Printf("zapgw: store error on idempotency (instagram-posts): %v", err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "unavailable", 0)
		return
	}
	if stored != "" {
		// Already published under this key: the SAME ids, no call to Meta.
		mediaID, containerID, _ := strings.Cut(stored, "|")
		writeJSON(w, http.StatusOK, instagramPostResponse{MediaID: mediaID, ContainerID: containerID, Kind: kind})
		return
	}
	if !reserved {
		respondError(w, http.StatusConflict, "retryable", "a publish with this key is in progress", 0)
		return
	}

	// Same reasoning as send(): the instance decides how long to wait for
	// Meta, not the consumer's client timeout — aborting mid-flight would
	// leave the key held with an unknown outcome. The poll alone (up to
	// 60 s) is longer than the default instance timeout, so the deadline is
	// the instance's plus the maximum poll wait.
	maxWait := h.timings.MaxWait
	if maxWait <= 0 {
		maxWait = meta.DefaultPublishMaxWait
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), InstanceDeadline(inst)+maxWait)
	defer cancel()

	images := make([]meta.InstagramPublishImage, len(p.Images))
	for i, img := range p.Images {
		images[i] = meta.InstagramPublishImage{URL: img.URL, AltText: img.AltText}
	}
	res, err := h.client.PublishInstagramPost(ctx, h.baseInstagram, inst.IgID, inst.SendToken, images, p.Caption, h.timings)
	if err != nil {
		var me *meta.MetaError
		var pe *meta.InstagramPublishError
		step := ""
		if errors.As(err, &pe) {
			step = pe.Step
		}
		// Known-negative: nothing was published. Everything before the
		// publish call, or a Meta refusal of it. Anything else at the
		// publish step may have gone out: HOLD the key.
		knownNegative := step != meta.PublishStepPublish || errors.As(err, &me)
		if knownNegative {
			if errRelease := h.store.ReleaseIdempotency(consumer.Name, key); errRelease != nil {
				log.Printf("ALARME zapgw: idempotency not released after a known-negative publish failure "+
					"(consumer=%q key=%q) — release it by hand or wait for the TTL purge: %v",
					consumer.Name, key, errRelease)
			}
		} else {
			log.Printf("ALARME zapgw: idempotency key HELD because the publish outcome is unknown "+
				"(the post may be live; consumer=%q key=%q): %v", consumer.Name, key, err)
		}
		respondInstagramPublishError(w, inst.Slug, step, pe, err)
		return
	}

	if err := h.store.ConfirmIdempotency(consumer.Name, key, res.MediaID+"|"+res.ContainerID); err != nil {
		log.Printf("ALARME zapgw: post %q published but idempotency not confirmed "+
			"(consumer=%q key=%q): %v", res.MediaID, consumer.Name, key, err)
	}
	writeJSON(w, http.StatusOK, instagramPostResponse{MediaID: res.MediaID, ContainerID: res.ContainerID, Kind: res.Kind})
}

func (h *InstagramPublishHandler) limit(w http.ResponseWriter, r *http.Request) {
	_, inst, ok := h.authorize(w, r, "instagram-publishing-limit")
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancel()

	lim, err := h.client.InstagramPublishingLimit(ctx, h.baseInstagram, inst.IgID, inst.SendToken)
	if err != nil {
		respondInstagramPublishError(w, inst.Slug, "", nil, err)
		return
	}
	writeJSON(w, http.StatusOK, instagramLimitResponse{
		QuotaUsage: lim.QuotaUsage, QuotaTotal: lim.QuotaTotal, QuotaDurationS: lim.QuotaDuration,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// publishErrorBody is the usual error body plus `step` and `status_code`.
type publishErrorBody struct {
	Error struct {
		Class           string `json:"class"`
		MetaCode        int    `json:"meta_code,omitempty"`
		Message         string `json:"message"`
		MetaDetail      string `json:"meta_detail,omitempty"`
		MetaSubcode     int    `json:"meta_subcode,omitempty"`
		MetaExplanation string `json:"meta_explanation,omitempty"`
		MetaTrace       string `json:"meta_trace,omitempty"`
		// Step: "container" | "status" | "publish". Only "publish" can have
		// made a post visible.
		Step string `json:"step,omitempty"`
		// StatusCode: the last container status_code, step "status" only.
		StatusCode string `json:"status_code,omitempty"`
	} `json:"error"`
}

// respondInstagramPublishError maps a failure of the flow, with the same
// Meta-error mapping as the other routes (statusForClass) plus the step.
func respondInstagramPublishError(w http.ResponseWriter, slug, step string, pe *meta.InstagramPublishError, err error) {
	var body publishErrorBody
	body.Error.Step = step
	status := http.StatusServiceUnavailable
	var me *meta.MetaError
	switch {
	case errors.As(err, &me):
		if me.Class == meta.ClassConfig {
			log.Printf("ALARME zapgw: Meta refused the credential of instance %q on the Instagram publishing flow", slug)
		}
		status = statusForClass(me.Class)
		body.Error.Class = string(me.Class)
		body.Error.Message = me.Message
		body.Error.MetaCode = me.MetaCode
		body.Error.MetaDetail = me.Detail
		body.Error.MetaSubcode = me.Subcode
		body.Error.MetaExplanation = me.Explanation
		body.Error.MetaTrace = me.Trace
	case errors.Is(err, meta.ErrInstagramPublishContainerFailed):
		status = http.StatusBadGateway
		body.Error.Class = string(meta.ClassUnknown)
		body.Error.Message = "the Instagram container did not reach FINISHED; nothing was published"
		if pe != nil {
			body.Error.StatusCode = pe.StatusCode
		}
	case errors.Is(err, meta.ErrInstagramPublishNotUnderstood):
		status = http.StatusBadGateway
		body.Error.Class = string(meta.ClassUnknown)
		body.Error.Message = "Meta answered the Instagram publishing call with something the gateway did not understand"
	case step == meta.PublishStepPublish:
		// Transport / deadline during media_publish: the post MAY be live.
		status = http.StatusBadGateway
		body.Error.Class = string(meta.ClassUnknown)
		body.Error.Message = "the outcome of the publish call is unknown: the post may be live; do not retry blindly"
	default:
		body.Error.Class = string(meta.ClassRetryable)
		body.Error.Message = "could not talk to Meta about the Instagram publishing; try again"
	}
	writeJSON(w, status, body)
}
