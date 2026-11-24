// GET /v1/instances/{slug}/instagram-insights and
// GET /v1/instances/{slug}/instagram-media (T-271)
//
// WHY THEY EXIST: a consumer that reports on an Instagram account needs
// numbers — reach, engagement, followers, and how each recent post did. The
// gateway only repeats what Meta says: no sum, no average, no cache. A
// metric Meta does not return is `null`, never 0 (Meta: missing data is an
// empty set, and a 0 would read as a measurement).
//
// Meta says insights can lag by up to 48 hours; that is Meta's, not ours.
//
// GUARD ORDER (the same as every instance route, authorizeInstagramRoute):
//
//	authenticate -> bond with the instance (403) -> instance exists (404)
//	-> instance active (503) -> instance type (400) -> query (400) -> Meta
package outbound

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/iscarelli/zapgw/internal/config"
	"github.com/iscarelli/zapgw/internal/meta"
)

const (
	defaultMediaLimit = 10
	maxMediaLimit     = 25
	maxCursorLen      = 512
)

type InstagramInsightsHandler struct {
	store         *config.Store
	auth          *Authenticator
	client        *meta.Client
	baseInstagram string
	types         AcceptedTypes
}

// NewInstagramInsightsHandler builds the routes against the real Instagram
// host. `types` is positional and mandatory (T-111).
func NewInstagramInsightsHandler(store *config.Store, auth *Authenticator, client *meta.Client, types AcceptedTypes) http.Handler {
	return NewInstagramInsightsHandlerWithBase(store, auth, client, meta.DefaultInstagramRenewalBase, types)
}

// NewInstagramInsightsHandlerWithBase has the host root injectable.
func NewInstagramInsightsHandlerWithBase(store *config.Store, auth *Authenticator, client *meta.Client, baseInstagram string, types AcceptedTypes) http.Handler {
	h := &InstagramInsightsHandler{store: store, auth: auth, client: client, baseInstagram: baseInstagram, types: types}
	mux := http.NewServeMux()
	// String LITERALS: the isolation table reads these registrations by text.
	mux.HandleFunc("GET /v1/instances/{slug}/instagram-insights", h.accountInsights)
	mux.HandleFunc("GET /v1/instances/{slug}/instagram-media", h.mediaList)
	return mux
}

type instagramInsightsResponse struct {
	Since          int64             `json:"since"`
	Until          int64             `json:"until"`
	FollowersCount *int64            `json:"followers_count"`
	MediaCount     *int64            `json:"media_count"`
	Metrics        map[string]*int64 `json:"metrics"`
}

type instagramInsightsError struct {
	Class       string `json:"class"`
	MetaCode    int    `json:"meta_code,omitempty"`
	MetaSubcode int    `json:"meta_subcode,omitempty"`
	Message     string `json:"message"`
}

type instagramMediaItemResponse struct {
	ID            string                  `json:"id"`
	MediaType     string                  `json:"media_type"`
	Timestamp     string                  `json:"timestamp"`
	Permalink     string                  `json:"permalink"`
	Caption       *string                 `json:"caption"`
	LikeCount     *int64                  `json:"like_count"`
	CommentsCount *int64                  `json:"comments_count"`
	Insights      map[string]*int64       `json:"insights"`
	InsightsError *instagramInsightsError `json:"insights_error,omitempty"`
}

type instagramMediaResponse struct {
	Items      []instagramMediaItemResponse `json:"items"`
	NextCursor *string                      `json:"next_cursor"`
}

// digitsOnly: a non-empty run of ASCII digits (no sign, no space).
func digitsOnly(s string) bool {
	return meta.IGSIDValid(s)
}

func (h *InstagramInsightsHandler) accountInsights(w http.ResponseWriter, r *http.Request) {
	_, inst, ok := authorizeInstagramRoute(h.store, h.auth, h.types, w, r, "instagram-insights")
	if !ok {
		return
	}
	q := r.URL.Query()
	sinceRaw, untilRaw := q.Get("since"), q.Get("until")
	if !digitsOnly(sinceRaw) || !digitsOnly(untilRaw) {
		respondError(w, http.StatusBadRequest, "permanent", "since and until are required and must be unix seconds (digits only)", 0)
		return
	}
	since, errSince := strconv.ParseInt(sinceRaw, 10, 64)
	until, errUntil := strconv.ParseInt(untilRaw, 10, 64)
	if errSince != nil || errUntil != nil {
		respondError(w, http.StatusBadRequest, "permanent", "since and until are required and must be unix seconds (digits only)", 0)
		return
	}
	if since >= until {
		respondError(w, http.StatusBadRequest, "permanent", "since must be earlier than until", 0)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancel()
	res, err := h.client.InstagramAccountInsights(ctx, h.baseInstagram, inst.IgID, inst.SendToken, since, until)
	if err != nil {
		respondInstagramInsightsError(w, inst.Slug, err)
		return
	}
	writeJSON(w, http.StatusOK, instagramInsightsResponse{
		Since: since, Until: until,
		FollowersCount: res.FollowersCount, MediaCount: res.MediaCount, Metrics: res.Metrics,
	})
}

func (h *InstagramInsightsHandler) mediaList(w http.ResponseWriter, r *http.Request) {
	_, inst, ok := authorizeInstagramRoute(h.store, h.auth, h.types, w, r, "instagram-media")
	if !ok {
		return
	}
	q := r.URL.Query()
	limit := defaultMediaLimit
	if q.Has("limit") {
		n, err := strconv.Atoi(q.Get("limit"))
		if !digitsOnly(q.Get("limit")) || err != nil || n < 1 || n > maxMediaLimit {
			respondError(w, http.StatusBadRequest, "permanent", "limit must be an integer from 1 to 25", 0)
			return
		}
		limit = n
	}
	after := q.Get("after")
	if len(after) > maxCursorLen {
		respondError(w, http.StatusBadRequest, "permanent", "after is too long", 0)
		return
	}

	listCtx, cancelList := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancelList()
	page, err := h.client.InstagramMediaList(listCtx, h.baseInstagram, inst.IgID, inst.SendToken, limit, after)
	if err != nil {
		respondInstagramInsightsError(w, inst.Slug, err)
		return
	}

	items := make([]instagramMediaItemResponse, len(page.Items))
	for i, it := range page.Items {
		items[i] = instagramMediaItemResponse{
			ID: it.ID, MediaType: it.MediaType, Timestamp: it.Timestamp, Permalink: it.Permalink,
			Caption: it.Caption, LikeCount: it.LikeCount, CommentsCount: it.CommentsCount,
		}
	}
	// One call per item, in parallel, under a deadline of its own: a slow or
	// failing item must not fail the page.
	insCtx, cancelIns := context.WithTimeout(r.Context(), InstanceDeadline(inst))
	defer cancelIns()
	var wg sync.WaitGroup
	for i := range items {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ins, err := h.client.InstagramMediaInsights(insCtx, h.baseInstagram, items[i].ID, inst.SendToken)
			if err != nil {
				items[i].InsightsError = insightsErrorOf(inst.Slug, err)
				return
			}
			items[i].Insights = ins
		}(i)
	}
	wg.Wait()

	resp := instagramMediaResponse{Items: items}
	if page.NextCursor != "" {
		cursor := page.NextCursor
		resp.NextCursor = &cursor
	}
	writeJSON(w, http.StatusOK, resp)
}

// insightsErrorOf is the per-item error: Meta's class/code/message when Meta
// refused, a generic line otherwise. Never the transport error (it carries
// the URL).
func insightsErrorOf(slug string, err error) *instagramInsightsError {
	var me *meta.MetaError
	if errors.As(err, &me) {
		if me.Class == meta.ClassConfig {
			log.Printf("ALARME zapgw: Meta refused the credential of instance %q when reading Instagram media insights", slug)
		}
		return &instagramInsightsError{Class: string(me.Class), MetaCode: me.MetaCode, MetaSubcode: me.Subcode, Message: me.Message}
	}
	if errors.Is(err, meta.ErrInstagramInsightsNotUnderstood) || errors.Is(err, meta.ErrInvalidPhoneNumberID) {
		return &instagramInsightsError{Class: string(meta.ClassUnknown),
			Message: "Meta answered the media insights call with something the gateway did not understand"}
	}
	return &instagramInsightsError{Class: string(meta.ClassRetryable),
		Message: "could not read the insights of this media; try again"}
}

// respondInstagramInsightsError: the same mapping as the other Instagram
// routes (respondMetaError + statusForClass).
func respondInstagramInsightsError(w http.ResponseWriter, slug string, err error) {
	var me *meta.MetaError
	switch {
	case errors.As(err, &me):
		if me.Class == meta.ClassConfig {
			log.Printf("ALARME zapgw: Meta refused the credential of instance %q when reading Instagram insights", slug)
		}
		respondMetaError(w, statusForClass(me.Class), me)
	case errors.Is(err, meta.ErrInstagramInsightsNotUnderstood):
		respondError(w, http.StatusBadGateway, string(meta.ClassUnknown),
			"Meta answered the Instagram insights call with something the gateway did not understand", 0)
	default:
		respondError(w, http.StatusServiceUnavailable, string(meta.ClassRetryable),
			"could not talk to Meta about the Instagram insights; try again", 0)
	}
}
