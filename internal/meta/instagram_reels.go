package meta

import (
	"context"
	"encoding/json"
	"net/url"
)

// T-273: Instagram Reels, published from a video URL.
//
// Source (read on 2026-11-24): the Instagram API with Instagram Login,
// "Content Publishing" page. The flow is the same three Graph calls as the
// feed post (see instagram_publish.go), but video processing at Meta takes
// minutes, so the three calls are exposed ONE AT A TIME here: the gateway
// does not poll in a loop for a Reel, the consumer drives the cadence by
// re-POSTing (see internal/outbound/instagram_reels_handler.go).
//
// Meta fetches the video ITSELF from the URL given. The gateway does not
// download or validate the video.

// InstagramReelParams are the fields of a Reel container. A nil pointer or an
// empty string means "do not send the field" (Meta's default).
type InstagramReelParams struct {
	VideoURL    string
	Caption     string
	CoverURL    string
	ThumbOffset *int64 // milliseconds
	ShareToFeed *bool
}

// CreateInstagramReelContainer creates the Reel container (step 1) and
// returns its id. Nothing is published. A failure is an
// *InstagramPublishError with step "container".
func (c *Client) CreateInstagramReelContainer(ctx context.Context, base, igID, token string, p InstagramReelParams) (string, error) {
	if !PhoneNumberIDValid(igID) {
		return "", &InstagramPublishError{Step: PublishStepContainer, Err: ErrInvalidPhoneNumberID}
	}
	body := map[string]any{"media_type": "REELS", "video_url": p.VideoURL}
	if p.Caption != "" {
		body["caption"] = p.Caption
	}
	if p.CoverURL != "" {
		body["cover_url"] = p.CoverURL
	}
	if p.ThumbOffset != nil {
		body["thumb_offset"] = *p.ThumbOffset
	}
	if p.ShareToFeed != nil {
		body["share_to_feed"] = *p.ShareToFeed
	}
	id, err := c.instagramPostID(ctx, base, token, []string{igID, "media"}, body)
	if err != nil {
		return "", &InstagramPublishError{Step: PublishStepContainer, Err: err}
	}
	return id, nil
}

// InstagramContainerStatus reads the container's status_code ONCE (step 2).
// A failure is an *InstagramPublishError with step "status".
func (c *Client) InstagramContainerStatus(ctx context.Context, base, token, containerID string) (string, error) {
	if !PhoneNumberIDValid(containerID) {
		return "", &InstagramPublishError{Step: PublishStepStatus, Err: ErrInvalidPhoneNumberID}
	}
	raw, err := c.instagramGet(ctx, base, token, []string{containerID}, url.Values{"fields": {"status_code"}})
	if err != nil {
		return "", &InstagramPublishError{Step: PublishStepStatus, Err: err}
	}
	var envelope struct {
		StatusCode string `json:"status_code"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.StatusCode == "" {
		return "", &InstagramPublishError{Step: PublishStepStatus, Err: ErrInstagramPublishNotUnderstood}
	}
	return envelope.StatusCode, nil
}

// PublishInstagramContainer calls media_publish for a FINISHED container
// (step 3) and returns the media id. This is the only call that makes
// anything visible. A failure is an *InstagramPublishError with step
// "publish".
func (c *Client) PublishInstagramContainer(ctx context.Context, base, igID, token, containerID string) (string, error) {
	if !PhoneNumberIDValid(igID) {
		return "", &InstagramPublishError{Step: PublishStepPublish, Err: ErrInvalidPhoneNumberID}
	}
	id, err := c.instagramPostID(ctx, base, token, []string{igID, "media_publish"},
		map[string]any{"creation_id": containerID})
	if err != nil {
		return "", &InstagramPublishError{Step: PublishStepPublish, Err: err}
	}
	return id, nil
}
