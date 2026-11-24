package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// T-269: Instagram user profile lookup by IGSID.
//
// Source (read on 2026-11-24): the Instagram Platform "User Profile API" of
// the Instagram API with Instagram Login —
// GET https://graph.instagram.com/<IGSID>?fields=name,username. It only works
// once the user gave consent, which Meta records when the user SENDS A
// MESSAGE to the account (or taps an icebreaker / persistent menu item). A
// comment does not grant it, and a user who blocked the business is not
// readable. Without consent Meta refuses with the text "User consent is
// required to access user profile." (the page documents no code/subcode, so
// the consumer gets whatever Meta sends, through the usual MetaError).

// ErrInvalidIGSID: the IGSID is not made only of digits. Checked BEFORE the
// URL is built, for the same reason PhoneNumberIDValid guards the send:
// url.JoinPath resolves `..`, and an id that comes from a consumer's request
// path is exactly the kind of value that must not be able to steer the path.
var ErrInvalidIGSID = errors.New("meta: invalid IGSID (digits only)")

// ErrInstagramProfileNotUnderstood: Meta answered 2xx but the body was not
// the JSON object this call expects, or carried no username.
var ErrInstagramProfileNotUnderstood = errors.New("meta: Instagram user profile response not understood")

// InstagramUserProfile is what the lookup returns. Name is nil when Meta
// sent `name` as null or omitted it (the field can be null).
type InstagramUserProfile struct {
	Username string
	Name     *string
}

// IGSIDValid reports whether `igsid` is a non-empty run of ASCII digits.
func IGSIDValid(igsid string) bool {
	if igsid == "" {
		return false
	}
	for _, r := range igsid {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// InstagramUserProfile reads `username` and `name` of the Instagram user
// `igsid`.
//
// `base` is a PARAMETER, never `c.base` — the SAME discipline as
// SendInstagramMessage (instagram.go): this endpoint lives on
// graph.instagram.com, a host different from the one `c.base` points to. The
// token goes in the Authorization header, never in the URL (Meta's page shows
// `?access_token=`, but this gateway never puts a token in a URL — see
// SendInstagramMessage), and is never logged.
//
// `username` comes back lowercased and without a leading `@`.
func (c *Client) InstagramUserProfile(ctx context.Context, base, igsid, token string) (InstagramUserProfile, error) {
	if !IGSIDValid(igsid) {
		return InstagramUserProfile{}, ErrInvalidIGSID
	}

	target, err := url.JoinPath(base, igsid)
	if err != nil {
		return InstagramUserProfile{}, fmt.Errorf("meta: build url: %w", err)
	}
	target += "?fields=" + url.QueryEscape("name,username")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return InstagramUserProfile{}, fmt.Errorf("meta: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		// Not interpolated: *url.Error carries the full URL, which here
		// carries the IGSID. Same rule as SendInstagramMessage.
		return InstagramUserProfile{}, fmt.Errorf("meta: transport failure while reading the Instagram user profile: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return InstagramUserProfile{}, fmt.Errorf("meta: read response: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return InstagramUserProfile{}, metaError
	}

	var envelope struct {
		Username *string `json:"username"`
		Name     *string `json:"name"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return InstagramUserProfile{}, ErrInstagramProfileNotUnderstood
	}
	username := ""
	if envelope.Username != nil {
		username = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(*envelope.Username), "@"))
	}
	if username == "" {
		// A 2xx without a username is as useless to the consumer as an
		// error, and must not read as a valid profile.
		return InstagramUserProfile{}, ErrInstagramProfileNotUnderstood
	}
	return InstagramUserProfile{Username: username, Name: envelope.Name}, nil
}
