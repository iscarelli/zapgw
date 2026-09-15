// The Resumable Upload API — the only way to get a `header_handle` for a
// template's `HEADER` example (T-241).
//
// WHY THIS IS A SEPARATE MECHANISM FROM UploadMedia (media.go): a
// `media_id`, from `POST /{phone_number_id}/media`, does NOT work as a
// template header handle. Creating a template with a HEADER of format
// IMAGE/VIDEO/DOCUMENT requires `example.header_handle` in the component,
// and that value only comes out of THIS flow. See
// docs/CONTRATO-CONSUMIDOR.md, the section next to "Send and download
// media", for what a consumer does with the result.
//
// SOURCE (read 2026-09-15): developers.facebook.com/docs/graph-api/guides/upload
//
//   - session: `POST /{app-id}/uploads?file_name=…&file_length=…&file_type=…`
//     -> `{"id": "upload:XXXX"}`
//   - bytes:   `POST /upload:XXXX`, headers `Authorization: OAuth <token>`
//     and `file_offset: 0`, body = raw bytes -> `{"h": "<handle>"}`
//   - mimes the doc lists: application/pdf, image/jpeg, image/jpg,
//     image/png, video/mp4 (the gateway's own accepted subset, narrower
//     than this, lives in internal/outbound/uploads_handler.go)
//
// THE AUTHORIZATION SCHEME CHANGES MID-FLOW, and that is Meta's doc, not a
// typo here: app-id discovery and session creation use the SAME
// `Authorization: Bearer <token>` as every other call in this client; only
// the byte-upload step uses `Authorization: OAuth <token>` — the exact
// spelling the Resumable Upload doc writes for that one endpoint.
package meta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

var (
	// ErrAppIDWithoutID: `GET /app` answered 2xx without a non-empty `id`.
	// SAME trap as ErrUploadWithoutID (media.go): a 2xx never proves the
	// field the caller needs actually came with it.
	ErrAppIDWithoutID = errors.New("meta: /app response without an id")
	// ErrUploadSessionWithoutID: the session-creation step (`POST
	// /{app-id}/uploads`) answered 2xx without a non-empty `id`.
	ErrUploadSessionWithoutID = errors.New("meta: upload session response without an id")
	// ErrUploadWithoutHandle: the byte-upload step answered 2xx without a
	// non-empty `h`. Deliberately NOT the same sentinel as media.go's
	// ErrUploadWithoutID (field `id`): the two calls answer with different
	// field names, and collapsing them into one error would erase which
	// field was missing.
	ErrUploadWithoutHandle = errors.New("meta: 2xx response without an upload handle")
)

// AppID asks the Graph API which app the token belongs to.
//
// 🔴 THE INSTANCE DOES NOT STORE AN APP ID (config.Instance,
// internal/config/store.go:328) — that's why it's discovered from the
// token on every call instead of being provisioned once: this route is
// rare (template creation), the cost of one extra call is zero, and
// storing it would be new state to keep in sync for no measured gain.
//
// This call, alone among this file's three, is what the outbound handler
// names explicitly when it fails (internal/outbound/uploads_handler.go) —
// it's the one part of this design that was never exercised against the
// real Meta.
func (c *Client) AppID(ctx context.Context, token string) (string, error) {
	target, err := url.JoinPath(c.base, "app")
	if err != nil {
		return "", fmt.Errorf("meta: build url: %w", err)
	}
	target += "?" + url.Values{"fields": {"id"}}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", fmt.Errorf("meta: build request: %w", err)
	}
	// The token goes in the HEADER, never in the URL: a token in a query
	// string leaks into proxy, server, and CDN logs.
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("meta: transport failure while discovering the app id: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return "", fmt.Errorf("meta: read app id response: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return "", metaError
	}
	return requiredStringField(raw, "id", ErrAppIDWithoutID)
}

// CreateUploadSession is the Resumable Upload API's second step: it opens
// a session for a file of the declared mime and length, and returns the
// session id (`upload:XXXX`) that CompleteUpload's `POST /upload:XXXX`
// needs.
//
// SPLIT FROM the byte-upload step (T-241 addendum) so the OUTBOUND HANDLER
// can attribute a failure to the RIGHT step — the consumer asked to tell
// "session creation failed" apart from "the bytes were rejected" without
// depending on Meta's own error prose, which is free to change.
func (c *Client) CreateUploadSession(
	ctx context.Context, appID, token, mimeType, fileName string, length int64,
) (string, error) {
	// THE SAME guard as every other call that turns an id into a URL
	// segment (client.go's PhoneNumberIDValid comment): `url.JoinPath`
	// resolves `..` like `path.Join`, so an unvalidated id could escape
	// the Graph API's version prefix.
	if !PhoneNumberIDValid(appID) {
		return "", ErrInvalidPhoneNumberID
	}

	target, err := url.JoinPath(c.base, appID, "uploads")
	if err != nil {
		return "", fmt.Errorf("meta: build url: %w", err)
	}
	target += "?" + url.Values{
		"file_name":   {fileName},
		"file_length": {strconv.FormatInt(length, 10)},
		"file_type":   {mimeType},
	}.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, nil)
	if err != nil {
		return "", fmt.Errorf("meta: build request: %w", err)
	}
	// Bearer here: only CompleteUpload, below, uses the doc's `OAuth`
	// spelling (see the package comment).
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("meta: transport failure while creating the upload session: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return "", fmt.Errorf("meta: read upload session response: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return "", metaError
	}
	return requiredStringField(raw, "id", ErrUploadSessionWithoutID)
}

// CompleteUpload is the Resumable Upload API's third and last step: it
// sends the bytes to the session CreateUploadSession opened, and returns
// the handle a template's `example.header_handle` needs.
//
// THE BYTES CROSS IN STREAMING: `content` becomes the request's body
// directly (`http.NewRequestWithContext` with `req.ContentLength` set) —
// no `io.ReadAll`, nothing written to disk. Media is content, and this
// project's line is "configuration yes, message never" (media.go).
func (c *Client) CompleteUpload(ctx context.Context, sessionID, token string, length int64, content io.Reader) (string, error) {
	// `sessionID` (`upload:XXXX`) IS the whole path segment — the doc's
	// `POST /upload:XXXX` names it directly, nothing is appended to it.
	target, err := url.JoinPath(c.base, sessionID)
	if err != nil {
		return "", fmt.Errorf("meta: build url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, content)
	if err != nil {
		return "", fmt.Errorf("meta: build request: %w", err)
	}
	req.ContentLength = length
	// `OAuth`, EXACTLY as the Resumable Upload doc writes it for this one
	// endpoint — every other call in this client, CreateUploadSession
	// included, uses `Bearer` (package comment, above).
	req.Header.Set("Authorization", "OAuth "+token)
	req.Header.Set("file_offset", "0")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("meta: transport failure while uploading the bytes: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return "", fmt.Errorf("meta: read upload response: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return "", metaError
	}
	return requiredStringField(raw, "h", ErrUploadWithoutHandle)
}

// requiredStringField reads `field` out of a JSON object body, requiring a
// non-empty string. THE SAME trap as media.go's uploadID, generalized to
// the three different field names this file reads (`id` twice, `h` once):
// a 2xx from Meta never proves the field it's supposed to carry actually
// came with it.
func requiredStringField(raw []byte, field string, missing error) (string, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", fmt.Errorf("%w: body not understood", missing)
	}
	rawValue, has := envelope[field]
	if !has {
		return "", missing
	}
	var value string
	if err := json.Unmarshal(rawValue, &value); err != nil {
		return "", fmt.Errorf("%w: body not understood", missing)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", missing
	}
	return value, nil
}
