package meta

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// T-270: Instagram feed publishing (single image and carousel) and the
// publishing quota.
//
// Source (read on 2026-11-24): the Instagram API with Instagram Login,
// "Content Publishing" and the IG User `content_publishing_limit` reference.
// The flow is three Graph calls, and only the last one makes anything
// visible:
//
//  1. POST /<ig_id>/media           -> creates a CONTAINER (nothing published)
//     (carousel: one container per item with is_carousel_item=true, then a
//     parent container with media_type=CAROUSEL and children=<ids>)
//  2. GET  /<container_id>?fields=status_code, until FINISHED
//  3. POST /<ig_id>/media_publish   -> publishes; returns the media id
//
// Meta fetches the image ITSELF from the URL given, so the URL has to be
// public for the whole call.
//
// Every call uses the host passed as `base` (graph.instagram.com), never
// c.base, and the token in the Authorization header — never in a URL, never
// logged: the same discipline as SendInstagramMessage.

// Publish steps, carried by InstagramPublishError.Step. Only PublishStepPublish
// can have made something visible on the account.
const (
	PublishStepContainer = "container"
	PublishStepStatus    = "status"
	PublishStepPublish   = "publish"
)

// Defaults for the container status poll. They are OURS, not Meta's: a
// container for an image normally finishes in seconds.
const (
	DefaultPublishPollInterval = 3 * time.Second
	DefaultPublishMaxWait      = 60 * time.Second
)

// ErrInstagramPublishContainerFailed: the container reached a terminal status
// other than FINISHED (ERROR, EXPIRED, ...), or never reached FINISHED within
// the maximum wait. media_publish was NOT called.
var ErrInstagramPublishContainerFailed = errors.New("meta: Instagram container did not reach FINISHED")

// ErrInstagramPublishNotUnderstood: Meta answered 2xx with a body that does
// not have what this step needs (no id, no status_code, no quota entry).
var ErrInstagramPublishNotUnderstood = errors.New("meta: Instagram publishing response not understood")

// InstagramPublishError says in which step the flow failed. Err is the cause
// (a *MetaError, ErrInstagramPublishContainerFailed, a transport error...).
type InstagramPublishError struct {
	Step string
	// StatusCode is the last status_code seen (set only for step "status").
	StatusCode string
	Err        error
}

func (e *InstagramPublishError) Error() string {
	return fmt.Sprintf("meta: instagram publish failed at step %s: %v", e.Step, e.Err)
}

func (e *InstagramPublishError) Unwrap() error { return e.Err }

// InstagramPublishImage is one image of a post.
type InstagramPublishImage struct {
	URL     string
	AltText string
}

// InstagramPublishTimings are the poll parameters; a zero field takes the
// default. Tests pass milliseconds.
type InstagramPublishTimings struct {
	PollInterval time.Duration
	MaxWait      time.Duration
}

// InstagramPublishResult is what a successful publish returns.
type InstagramPublishResult struct {
	MediaID     string
	ContainerID string
	// Kind is "image" or "carousel".
	Kind string
}

// InstagramPublishingLimit is the quota passthrough. Nothing here is a
// number of ours: Meta's own pages disagree about the total, so the gateway
// only repeats what Meta says.
type InstagramPublishingLimit struct {
	QuotaUsage    int
	QuotaTotal    int
	QuotaDuration int
}

// PublishInstagramPost publishes `images` (1 = single image, 2..20 =
// carousel; the count is validated by the caller) with `caption` on the
// parent only.
func (c *Client) PublishInstagramPost(
	ctx context.Context, base, igID, token string,
	images []InstagramPublishImage, caption string, timings InstagramPublishTimings,
) (InstagramPublishResult, error) {
	if !PhoneNumberIDValid(igID) {
		return InstagramPublishResult{}, &InstagramPublishError{Step: PublishStepContainer, Err: ErrInvalidPhoneNumberID}
	}
	if timings.PollInterval <= 0 {
		timings.PollInterval = DefaultPublishPollInterval
	}
	if timings.MaxWait <= 0 {
		timings.MaxWait = DefaultPublishMaxWait
	}
	fail := func(step string, err error) error {
		return &InstagramPublishError{Step: step, Err: err}
	}

	var containerID, kind string
	if len(images) == 1 {
		kind = "image"
		body := map[string]any{"image_url": images[0].URL}
		if images[0].AltText != "" {
			body["alt_text"] = images[0].AltText
		}
		if caption != "" {
			body["caption"] = caption
		}
		id, err := c.instagramPostID(ctx, base, token, []string{igID, "media"}, body)
		if err != nil {
			return InstagramPublishResult{}, fail(PublishStepContainer, err)
		}
		containerID = id
	} else {
		kind = "carousel"
		children := make([]string, 0, len(images))
		for _, img := range images {
			body := map[string]any{"image_url": img.URL, "is_carousel_item": true}
			if img.AltText != "" {
				body["alt_text"] = img.AltText
			}
			id, err := c.instagramPostID(ctx, base, token, []string{igID, "media"}, body)
			if err != nil {
				return InstagramPublishResult{}, fail(PublishStepContainer, err)
			}
			children = append(children, id)
		}
		body := map[string]any{"media_type": "CAROUSEL", "children": strings.Join(children, ",")}
		if caption != "" {
			body["caption"] = caption
		}
		id, err := c.instagramPostID(ctx, base, token, []string{igID, "media"}, body)
		if err != nil {
			return InstagramPublishResult{}, fail(PublishStepContainer, err)
		}
		containerID = id
	}

	if statusCode, err := c.waitInstagramContainer(ctx, base, token, containerID, timings); err != nil {
		return InstagramPublishResult{}, &InstagramPublishError{Step: PublishStepStatus, StatusCode: statusCode, Err: err}
	}

	mediaID, err := c.instagramPostID(ctx, base, token, []string{igID, "media_publish"},
		map[string]any{"creation_id": containerID})
	if err != nil {
		return InstagramPublishResult{}, fail(PublishStepPublish, err)
	}
	return InstagramPublishResult{MediaID: mediaID, ContainerID: containerID, Kind: kind}, nil
}

// instagramPostID POSTs a JSON body to base/<elems...> and returns the `id`
// of the answer.
func (c *Client) instagramPostID(ctx context.Context, base, token string, elems []string, body map[string]any) (string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("meta: build body: %w", err)
	}
	target, err := url.JoinPath(base, elems...)
	if err != nil {
		return "", fmt.Errorf("meta: build url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("meta: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	answer, err := c.instagramDo(req)
	if err != nil {
		return "", err
	}
	var envelope struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(answer, &envelope); err != nil || !PhoneNumberIDValid(envelope.ID) {
		return "", ErrInstagramPublishNotUnderstood
	}
	return envelope.ID, nil
}

// instagramGet GETs base/<elems...>?<query> and returns the body of a 2xx.
func (c *Client) instagramGet(ctx context.Context, base, token string, elems []string, query url.Values) ([]byte, error) {
	target, err := url.JoinPath(base, elems...)
	if err != nil {
		return nil, fmt.Errorf("meta: build url: %w", err)
	}
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("meta: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return c.instagramDo(req)
}

// instagramDo sends the request and returns the body of a 2xx, or the
// classified *MetaError. The transport error is not interpolated: *url.Error
// carries the full URL, which here carries the ig_id or a container id.
func (c *Client) instagramDo(req *http.Request) ([]byte, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("meta: transport failure while talking to Instagram: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return nil, fmt.Errorf("meta: read response: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return nil, metaError
	}
	return raw, nil
}

// waitInstagramContainer polls the container status until FINISHED. It
// returns the last status_code seen alongside any error.
func (c *Client) waitInstagramContainer(ctx context.Context, base, token, containerID string, t InstagramPublishTimings) (string, error) {
	deadline := time.Now().Add(t.MaxWait)
	last := ""
	for {
		raw, err := c.instagramGet(ctx, base, token, []string{containerID}, url.Values{"fields": {"status_code"}})
		if err != nil {
			return last, err
		}
		var envelope struct {
			StatusCode string `json:"status_code"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil || envelope.StatusCode == "" {
			return last, ErrInstagramPublishNotUnderstood
		}
		last = envelope.StatusCode
		switch last {
		case "FINISHED":
			return last, nil
		case "IN_PROGRESS":
			// keep waiting
		default:
			// ERROR, EXPIRED, PUBLISHED or anything unknown: not publishable.
			return last, ErrInstagramPublishContainerFailed
		}
		if time.Now().Add(t.PollInterval).After(deadline) {
			return last, ErrInstagramPublishContainerFailed
		}
		timer := time.NewTimer(t.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, ctx.Err()
		case <-timer.C:
		}
	}
}

// InstagramPublishingLimit reads the account's publishing quota.
func (c *Client) InstagramPublishingLimit(ctx context.Context, base, igID, token string) (InstagramPublishingLimit, error) {
	if !PhoneNumberIDValid(igID) {
		return InstagramPublishingLimit{}, ErrInvalidPhoneNumberID
	}
	raw, err := c.instagramGet(ctx, base, token, []string{igID, "content_publishing_limit"},
		url.Values{"fields": {"config,quota_usage"}})
	if err != nil {
		return InstagramPublishingLimit{}, err
	}
	var envelope struct {
		Data []struct {
			QuotaUsage *int `json:"quota_usage"`
			Config     *struct {
				QuotaTotal    *int `json:"quota_total"`
				QuotaDuration *int `json:"quota_duration"`
			} `json:"config"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope.Data) == 0 {
		return InstagramPublishingLimit{}, ErrInstagramPublishNotUnderstood
	}
	d := envelope.Data[0]
	if d.QuotaUsage == nil || d.Config == nil || d.Config.QuotaTotal == nil || d.Config.QuotaDuration == nil {
		return InstagramPublishingLimit{}, ErrInstagramPublishNotUnderstood
	}
	return InstagramPublishingLimit{
		QuotaUsage: *d.QuotaUsage, QuotaTotal: *d.Config.QuotaTotal, QuotaDuration: *d.Config.QuotaDuration,
	}, nil
}
