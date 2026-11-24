// POST /v1/instances/{slug}/instagram-reels (T-273)
//
// WHY IT EXISTS: a consumer that publishes Reels from MP4s it already hosts.
// Video processing at Meta takes minutes — too long for one synchronous call
// like instagram-posts — so the route is ASYNC and RESUMED BY RE-POST: the
// consumer repeats the same request with the same Idempotency-Key until it
// gets 200 or an error. Each request makes at most ONE status GET at Meta;
// there is no polling loop inside a request.
//
// STATE MACHINE, kept in the idempotency row's stored value (the same
// `wa_message_id` column instagram-posts uses):
//
//	""                       reserved, in flight        -> 409 retryable
//	reel-container:<cid>     container made, not published
//	reel-publishing:<cid>    media_publish being called / outcome unknown
//	<media_id>|<cid>         published (same shape as instagram-posts)
//
// SwapIdempotencyResult (compare-and-swap) is the ONLY guard against two
// concurrent re-POSTs both calling media_publish: only the request that wins
// the swap reel-container -> reel-publishing may call it. From
// reel-publishing nobody ever calls media_publish again: the reel may be
// live, and a duplicate on a public feed is the defect this guards.
//
// GUARD ORDER: the same as instagram-posts (authorizeInstagramRoute).
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
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/httpx"
	"github.com/iscarelli/zapgw/internal/meta"
)

const (
	reelContainerPrefix  = "reel-container:"
	reelPublishingPrefix = "reel-publishing:"
	// reelRetryAfterSeconds is the cadence we ask the consumer for. Meta
	// recommends polling a container once a minute.
	reelRetryAfterSeconds = 60
)

type instagramReelRequest struct {
	VideoURL      string `json:"video_url"`
	Caption       string `json:"caption,omitempty"`
	CoverURL      string `json:"cover_url,omitempty"`
	ThumbOffsetMs *int64 `json:"thumb_offset_ms,omitempty"`
	ShareToFeed   *bool  `json:"share_to_feed,omitempty"`
}

type instagramReelResponse struct {
	MediaID     string `json:"media_id"`
	ContainerID string `json:"container_id"`
}

type instagramReelPending struct {
	ContainerID string `json:"container_id"`
	Status      string `json:"status"`
	RetryAfterS int    `json:"retry_after_s"`
}

// instagramReelHash covers the slug, a route marker and EVERY body field.
func instagramReelHash(slug string, p instagramReelRequest) string {
	raw, _ := json.Marshal(struct {
		Route    string `json:"route"`
		Instance string `json:"instance"`
		Request  instagramReelRequest
	}{"instagram-reels", slug, p})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func absoluteHTTPS(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

// validate trims in place and returns the consumer-facing message of the
// first problem, or "".
func (p *instagramReelRequest) validate() string {
	p.VideoURL = strings.TrimSpace(p.VideoURL)
	p.CoverURL = strings.TrimSpace(p.CoverURL)
	p.Caption = strings.TrimSpace(p.Caption)
	if !absoluteHTTPS(p.VideoURL) {
		return "video_url must be an absolute https:// URL"
	}
	if p.CoverURL != "" && !absoluteHTTPS(p.CoverURL) {
		return "cover_url must be an absolute https:// URL"
	}
	if p.ThumbOffsetMs != nil && *p.ThumbOffsetMs < 0 {
		return "thumb_offset_ms must be an integer >= 0"
	}
	if p.CoverURL != "" && p.ThumbOffsetMs != nil {
		return "send cover_url or thumb_offset_ms, not both"
	}
	if utf8.RuneCountInString(p.Caption) > maxCaptionRune {
		return "caption must have at most 2200 characters"
	}
	return ""
}

func (h *InstagramPublishHandler) reels(w http.ResponseWriter, r *http.Request) {
	consumer, inst, ok := h.authorize(w, r, "instagram-reels")
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
	var p instagramReelRequest
	if err := json.Unmarshal(raw, &p); err != nil {
		respondError(w, http.StatusBadRequest, "permanent", "body is not valid JSON", 0)
		return
	}
	if msg := p.validate(); msg != "" {
		respondError(w, http.StatusBadRequest, "permanent", msg, 0)
		return
	}

	stored, reserved, err := h.store.ReserveIdempotency(consumer.Name, key, instagramReelHash(inst.Slug, p))
	if err != nil {
		if errors.Is(err, config.ErrKeyWithDifferentRequest) {
			respondError(w, http.StatusUnprocessableEntity, "permanent",
				"this idempotency key was already used for a different request", 0)
			return
		}
		log.Printf("zapgw: store error on idempotency (instagram-reels): %v", err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "unavailable", 0)
		return
	}

	// Like instagram-posts: the instance decides how long to wait for Meta,
	// not the consumer's client timeout.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), InstanceDeadline(inst))
	defer cancel()

	switch {
	case reserved:
		containerID, err := h.client.CreateInstagramReelContainer(ctx, h.baseInstagram, inst.IgID, inst.SendToken,
			meta.InstagramReelParams{VideoURL: p.VideoURL, Caption: p.Caption, CoverURL: p.CoverURL,
				ThumbOffset: p.ThumbOffsetMs, ShareToFeed: p.ShareToFeed})
		if err != nil {
			h.releaseReelKey(consumer.Name, key, "container creation failed")
			respondInstagramPublishError(w, inst.Slug, meta.PublishStepContainer, peOf(err), err)
			return
		}
		if err := h.store.ConfirmIdempotency(consumer.Name, key, reelContainerPrefix+containerID); err != nil {
			// The container exists but this key can never resume it. It
			// expires at Meta on its own; nothing was published.
			log.Printf("ALARME zapgw: reel container %q created but idempotency not confirmed "+
				"(consumer=%q key=%q): %v", containerID, consumer.Name, key, err)
			h.releaseReelKey(consumer.Name, key, "confirm failed")
			respondError(w, http.StatusServiceUnavailable, "retryable", "unavailable", 0)
			return
		}
		h.advanceReel(ctx, w, consumer, inst, key, containerID)
	case stored == "":
		respondError(w, http.StatusConflict, "retryable", "a publish with this key is in progress", 0)
	case strings.HasPrefix(stored, reelPublishingPrefix):
		respondError(w, http.StatusConflict, "permanent",
			"the publish outcome is unknown: the reel may be live; check the account before using a new key", 0)
	case strings.HasPrefix(stored, reelContainerPrefix):
		h.advanceReel(ctx, w, consumer, inst, key, strings.TrimPrefix(stored, reelContainerPrefix))
	default:
		// Already published under this key: the SAME ids, no call to Meta.
		mediaID, containerID, _ := strings.Cut(stored, "|")
		writeJSON(w, http.StatusOK, instagramReelResponse{MediaID: mediaID, ContainerID: containerID})
	}
}

// peOf extracts the *InstagramPublishError of err, or nil.
func peOf(err error) *meta.InstagramPublishError {
	var pe *meta.InstagramPublishError
	if errors.As(err, &pe) {
		return pe
	}
	return nil
}

func (h *InstagramPublishHandler) releaseReelKey(consumerName, key, why string) {
	if err := h.store.ReleaseIdempotency(consumerName, key); err != nil {
		log.Printf("ALARME zapgw: idempotency not released (%s; consumer=%q key=%q) — release it by hand "+
			"or wait for the TTL purge: %v", why, consumerName, key, err)
	}
}

// advanceReel does ONE status GET for the container and acts on it.
func (h *InstagramPublishHandler) advanceReel(ctx context.Context, w http.ResponseWriter, consumer config.Consumer, inst config.Instance, key, containerID string) {
	status, err := h.client.InstagramContainerStatus(ctx, h.baseInstagram, inst.SendToken, containerID)
	if err != nil {
		// Nothing was published and the row stays as it is: the consumer
		// may re-POST.
		respondInstagramPublishError(w, inst.Slug, meta.PublishStepStatus, peOf(err), err)
		return
	}
	switch status {
	case "IN_PROGRESS":
		w.Header().Set("Retry-After", strconv.Itoa(reelRetryAfterSeconds))
		writeJSON(w, http.StatusAccepted, instagramReelPending{
			ContainerID: containerID, Status: status, RetryAfterS: reelRetryAfterSeconds})
	case "FINISHED":
		h.publishReel(ctx, w, consumer, inst, key, containerID)
	default:
		failure := &meta.InstagramPublishError{
			Step: meta.PublishStepStatus, StatusCode: status, Err: meta.ErrInstagramPublishContainerFailed}
		// ERROR / EXPIRED: the container is dead and nothing was published,
		// so the key is released (a new POST makes a new container). Any
		// other status (PUBLISHED, unknown) is NOT proof that nothing is
		// live: the row is kept.
		if status == "ERROR" || status == "EXPIRED" {
			h.releaseReelKey(consumer.Name, key, "container "+status)
		}
		respondInstagramPublishError(w, inst.Slug, meta.PublishStepStatus, failure, failure)
	}
}

// publishReel runs the guarded media_publish for a FINISHED container.
func (h *InstagramPublishHandler) publishReel(ctx context.Context, w http.ResponseWriter, consumer config.Consumer, inst config.Instance, key, containerID string) {
	won, err := h.store.SwapIdempotencyResult(consumer.Name, key, reelContainerPrefix+containerID, reelPublishingPrefix+containerID)
	if err != nil {
		log.Printf("zapgw: store error on idempotency swap (instagram-reels): %v", err)
		respondError(w, http.StatusServiceUnavailable, "retryable", "unavailable", 0)
		return
	}
	if !won {
		respondError(w, http.StatusConflict, "retryable", "a publish with this key is in progress", 0)
		return
	}

	mediaID, err := h.client.PublishInstagramContainer(ctx, h.baseInstagram, inst.IgID, inst.SendToken, containerID)
	if err != nil {
		var me *meta.MetaError
		if errors.As(err, &me) {
			// Meta said no: nothing was published and the container is still
			// valid, so a re-POST may try again.
			if _, errSwap := h.store.SwapIdempotencyResult(consumer.Name, key,
				reelPublishingPrefix+containerID, reelContainerPrefix+containerID); errSwap != nil {
				log.Printf("ALARME zapgw: reel key left in publishing state after a Meta refusal "+
					"(consumer=%q key=%q): %v", consumer.Name, key, errSwap)
			}
		} else {
			log.Printf("ALARME zapgw: reel key HELD because the publish outcome is unknown "+
				"(the reel may be live; consumer=%q key=%q): %v", consumer.Name, key, err)
		}
		respondInstagramPublishError(w, inst.Slug, meta.PublishStepPublish, peOf(err), err)
		return
	}

	if ok, err := h.store.SwapIdempotencyResult(consumer.Name, key,
		reelPublishingPrefix+containerID, mediaID+"|"+containerID); err != nil || !ok {
		log.Printf("ALARME zapgw: reel %q published but idempotency not confirmed "+
			"(consumer=%q key=%q ok=%v): %v", mediaID, consumer.Name, key, ok, err)
	}
	writeJSON(w, http.StatusOK, instagramReelResponse{MediaID: mediaID, ContainerID: containerID})
}
