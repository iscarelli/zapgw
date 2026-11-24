package meta

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// T-271: Instagram account insights and the media list with per-post metrics.
//
// Source (read on 2026-11-24): the Instagram API with Instagram Login
// references "IG User > insights", "IG User > media" and "IG Media >
// insights". Same host, token and error rules as the publishing flow
// (instagram_publish.go): `base` is graph.instagram.com, the token is in the
// Authorization header, never in a URL, never logged.
//
// The gateway computes NOTHING: a number is Meta's `total_value.value` (or
// the single `values[0].value` of a media metric) passed through, and a
// metric Meta omits is nil (JSON null), never 0 — Meta documents "missing
// data" as an empty set, and a 0 would read as a measurement.

// AccountInsightMetrics are the metrics asked of the account, in the order
// they are sent. `impressions` is deprecated by Meta and not asked; Meta's
// current table has no account-level "profile views" (closest:
// profile_links_taps).
var AccountInsightMetrics = []string{
	"reach", "accounts_engaged", "views", "total_interactions", "follows_and_unfollows", "profile_links_taps",
}

// MediaInsightMetrics are the metrics asked of each media item.
var MediaInsightMetrics = []string{
	"reach", "saved", "shares", "views", "total_interactions", "profile_visits",
}

// The docs flag `caption` as Facebook-Login-only, so the media list retries
// once without it when Meta refuses the first field list.
const (
	MediaListFieldsWithCaption    = "id,media_type,timestamp,permalink,caption,like_count,comments_count"
	MediaListFieldsWithoutCaption = "id,media_type,timestamp,permalink,like_count,comments_count"
)

// ErrInstagramInsightsNotUnderstood: Meta answered 2xx with a body that is
// not the JSON this call expects.
var ErrInstagramInsightsNotUnderstood = errors.New("meta: Instagram insights response not understood")

// InstagramAccountInsights is the account answer. A nil value is null.
type InstagramAccountInsights struct {
	FollowersCount *int64
	MediaCount     *int64
	Metrics        map[string]*int64
}

// InstagramMediaItem is one entry of the media list. Caption, LikeCount and
// CommentsCount are nil when Meta omitted them.
type InstagramMediaItem struct {
	ID            string
	MediaType     string
	Timestamp     string
	Permalink     string
	Caption       *string
	LikeCount     *int64
	CommentsCount *int64
}

// InstagramMediaPage is one page of the list. NextCursor is empty on the
// last page.
type InstagramMediaPage struct {
	Items      []InstagramMediaItem
	NextCursor string
}

// parseInsightsData reads `{"data":[{"name":..,"total_value":{"value":N}}]}`
// or the per-media shape `{"data":[{"name":..,"values":[{"value":N}]}]}`.
// Only the names in `want` are kept; a name Meta did not send, or sent with
// no integer value, is nil.
func parseInsightsData(raw []byte, want []string) (map[string]*int64, error) {
	var envelope struct {
		Data []struct {
			Name       string `json:"name"`
			TotalValue *struct {
				Value json.RawMessage `json:"value"`
			} `json:"total_value"`
			Values []struct {
				Value json.RawMessage `json:"value"`
			} `json:"values"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, ErrInstagramInsightsNotUnderstood
	}
	out := make(map[string]*int64, len(want))
	for _, name := range want {
		out[name] = nil
	}
	for _, d := range envelope.Data {
		if _, asked := out[d.Name]; !asked {
			continue
		}
		var rawValue json.RawMessage
		switch {
		case d.TotalValue != nil:
			rawValue = d.TotalValue.Value
		case len(d.Values) > 0:
			rawValue = d.Values[0].Value
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(string(rawValue)), 10, 64); err == nil {
			v := n
			out[d.Name] = &v
		}
	}
	return out, nil
}

// InstagramAccountInsights reads the account metrics for [since, until]
// (unix seconds, validated by the caller) and the follower/media counts.
func (c *Client) InstagramAccountInsights(ctx context.Context, base, igID, token string, since, until int64) (InstagramAccountInsights, error) {
	if !PhoneNumberIDValid(igID) {
		return InstagramAccountInsights{}, ErrInvalidPhoneNumberID
	}
	raw, err := c.instagramGet(ctx, base, token, []string{igID, "insights"}, url.Values{
		"metric":      {strings.Join(AccountInsightMetrics, ",")},
		"period":      {"day"},
		"metric_type": {"total_value"},
		"since":       {strconv.FormatInt(since, 10)},
		"until":       {strconv.FormatInt(until, 10)},
	})
	if err != nil {
		return InstagramAccountInsights{}, err
	}
	metrics, err := parseInsightsData(raw, AccountInsightMetrics)
	if err != nil {
		return InstagramAccountInsights{}, err
	}

	rawMe, err := c.instagramGet(ctx, base, token, []string{"me"},
		url.Values{"fields": {"followers_count,media_count"}})
	if err != nil {
		return InstagramAccountInsights{}, err
	}
	var me struct {
		FollowersCount *int64 `json:"followers_count"`
		MediaCount     *int64 `json:"media_count"`
	}
	if err := json.Unmarshal(rawMe, &me); err != nil {
		return InstagramAccountInsights{}, ErrInstagramInsightsNotUnderstood
	}
	return InstagramAccountInsights{FollowersCount: me.FollowersCount, MediaCount: me.MediaCount, Metrics: metrics}, nil
}

// InstagramMediaList reads one page of the account's media. If Meta refuses
// the field list that includes `caption`, it retries ONCE without it (the
// caption is then nil); if that also fails, the second error is returned.
func (c *Client) InstagramMediaList(ctx context.Context, base, igID, token string, limit int, after string) (InstagramMediaPage, error) {
	if !PhoneNumberIDValid(igID) {
		return InstagramMediaPage{}, ErrInvalidPhoneNumberID
	}
	get := func(fields string) ([]byte, error) {
		q := url.Values{"fields": {fields}, "limit": {strconv.Itoa(limit)}}
		if after != "" {
			q.Set("after", after)
		}
		return c.instagramGet(ctx, base, token, []string{igID, "media"}, q)
	}
	raw, err := get(MediaListFieldsWithCaption)
	var me *MetaError
	if errors.As(err, &me) {
		raw, err = get(MediaListFieldsWithoutCaption)
	}
	if err != nil {
		return InstagramMediaPage{}, err
	}
	var envelope struct {
		Data []struct {
			ID            string  `json:"id"`
			MediaType     string  `json:"media_type"`
			Timestamp     string  `json:"timestamp"`
			Permalink     string  `json:"permalink"`
			Caption       *string `json:"caption"`
			LikeCount     *int64  `json:"like_count"`
			CommentsCount *int64  `json:"comments_count"`
		} `json:"data"`
		Paging struct {
			Cursors struct {
				After string `json:"after"`
			} `json:"cursors"`
			Next string `json:"next"`
		} `json:"paging"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return InstagramMediaPage{}, ErrInstagramInsightsNotUnderstood
	}
	page := InstagramMediaPage{Items: make([]InstagramMediaItem, 0, len(envelope.Data))}
	for _, d := range envelope.Data {
		page.Items = append(page.Items, InstagramMediaItem{
			ID: d.ID, MediaType: d.MediaType, Timestamp: d.Timestamp, Permalink: d.Permalink,
			Caption: d.Caption, LikeCount: d.LikeCount, CommentsCount: d.CommentsCount,
		})
	}
	// A cursor with no `next` is the last page: handing it out would invite
	// a consumer to page forever.
	if envelope.Paging.Next != "" {
		page.NextCursor = envelope.Paging.Cursors.After
	}
	return page, nil
}

// InstagramMediaInsights reads the per-post metrics of one media id. Meta
// rejects the WHOLE request (code 100) when one metric is unsupported by the
// media product type (measured 2026-11-24: profile_visits on Reels), so on a
// code-100 refusal it retries ONCE without profile_visits; that metric is then
// nil. If the retry fails too, the retry's error is returned. Any other error
// is not retried.
func (c *Client) InstagramMediaInsights(ctx context.Context, base, mediaID, token string) (map[string]*int64, error) {
	if !PhoneNumberIDValid(mediaID) {
		return nil, ErrInvalidPhoneNumberID
	}
	get := func(metrics []string) ([]byte, error) {
		return c.instagramGet(ctx, base, token, []string{mediaID, "insights"},
			url.Values{"metric": {strings.Join(metrics, ",")}})
	}
	raw, err := get(MediaInsightMetrics)
	var me *MetaError
	if errors.As(err, &me) && me.MetaCode == 100 {
		without := make([]string, 0, len(MediaInsightMetrics))
		for _, m := range MediaInsightMetrics {
			if m != "profile_visits" {
				without = append(without, m)
			}
		}
		raw, err = get(without)
	}
	if err != nil {
		return nil, err
	}
	return parseInsightsData(raw, MediaInsightMetrics)
}
