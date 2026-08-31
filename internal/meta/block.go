// Blocking users — POST/DELETE/GET <phone_number_id>/block_users (T-148,
// owner's decision on 2026-08-20).
//
// The Cloud API has its own endpoint for the business to stop RECEIVING
// from an abusive number, and the gateway didn't reach it — a consumer who
// needed this had no path, and under this house's rule ("NINGUÉM fala direto
// com a Meta") couldn't go direct either. Source:
// developers.facebook.com/docs/whatsapp/cloud-api/block-users/, read
// 2026-08-20.
//
// 🔴 THIS FILE'S HARD CASE IS PARTIAL SUCCESS: Meta answers `200` at the
// call's ENVELOPE and reports an error PER NUMBER inside it
// (`block_users.failed_users`) — 1,000 numbers can become 998 blocked and 2
// rejected, ALL under the same `200`. `ClassifyResponse` only sees the
// envelope's STATUS; it cannot see this, and shouldn't: whoever returns the
// per-number verdict is blockResult, called AFTER
// ClassifyResponse has already confirmed the whole envelope isn't an
// error.
//
// The restriction on WHO can be blocked (only someone who messaged in the
// last 24h; never another business account) is Meta's OWN, applied per
// number inside the same partial-failure mechanism — this file doesn't
// reimplement it, it just passes through the error it returns for that
// number (see BlockFailure).
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
	"strconv"
)

// ErrBlockResponseNotUnderstood: Meta answered 2xx with a body that
// doesn't have the minimum expected shape (neither `block_users`, nor
// `data`/`paging`, depending on the call).
var ErrBlockResponseNotUnderstood = errors.New("meta: resposta de bloqueio nao entendida")

// BlockedUser is a number that ENTERED or LEFT the block list
// successfully — the `input` (phone number we sent) and the `wa_id` Meta
// returned for it.
type BlockedUser struct {
	Phone string
	WaID  string
}

// BlockFailure is a number Meta REJECTED INSIDE the same `200` envelope —
// this task's hard case (T-148, item 3). `MetaCode`/`Message`/`Detail`
// are the SAME THREE fields MetaError exposes for a WHOLE-ENVELOPE failure
// (the same reading of error.message/code/error_data.details), just per
// item here.
type BlockFailure struct {
	Phone    string
	WaID     string
	MetaCode int
	Message  string
	Detail   string
}

// BlockResult is the PER-NUMBER verdict of a POST or DELETE
// /block_users. Succeeded and Failed together cover the numbers Meta
// processed; neither one is authorized, alone or as `nil`, to mean
// "everything went fine" — whoever reads the result checks Failed, never
// just the call's error.
type BlockResult struct {
	Succeeded []BlockedUser
	Failed    []BlockFailure
}

// blockUser is the item in the POST/DELETE body — just the `user`
// field Meta asks for.
type blockUser struct {
	User string `json:"user"`
}

// succeededItemMeta is `added_users[i]` or `removed_users[i]` from the
// response.
type succeededItemMeta struct {
	Input string `json:"input"`
	WaID  string `json:"wa_id"`
}

// failedItemMeta is `failed_users[i]` from the response. `Errors` is a list
// because that's how Meta models it, but only the FIRST error per number is
// passed through — the same decision as ClassifyResponse, which only
// reads ONE error per call.
type failedItemMeta struct {
	Input  string `json:"input"`
	WaID   string `json:"wa_id"`
	Errors []struct {
		Message   string          `json:"message"`
		Code      json.RawMessage `json:"code"`
		ErrorData struct {
			Details string `json:"details"`
		} `json:"error_data"`
	} `json:"errors"`
}

// callBlockUsers is the SHARED BODY of BlockUsers and
// UnblockUsers — the ONLY difference between the two calls is the
// HTTP verb (the body is IDENTICAL in both, confirmed against the source
// cited at the top of this file). Two copies would diverge at the first
// change — this project's mother trap.
func (c *Client) callBlockUsers(
	ctx context.Context, method, phoneNumberID, token string, phones []string,
) (BlockResult, error) {
	if !PhoneNumberIDValid(phoneNumberID) {
		return BlockResult{}, ErrInvalidPhoneNumberID
	}

	items := make([]blockUser, 0, len(phones))
	for _, t := range phones {
		items = append(items, blockUser{User: t})
	}
	body := map[string]any{
		"messaging_product": "whatsapp",
		"block_users":       items,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return BlockResult{}, fmt.Errorf("meta: montar corpo do bloqueio: %w", err)
	}

	target, err := url.JoinPath(c.base, phoneNumberID, "block_users")
	if err != nil {
		return BlockResult{}, fmt.Errorf("meta: montar url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(raw))
	if err != nil {
		return BlockResult{}, fmt.Errorf("meta: montar requisicao: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// The token goes in the HEADER, never in the URL: a token in a query
	// string leaks into proxy, server, and CDN logs.
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		// We do NOT interpolate the error: *url.Error carries the full
		// URL, and it carries the client's phone_number_id.
		return BlockResult{}, fmt.Errorf("meta: falha de transporte ao falar com block_users: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	rawResponse, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return BlockResult{}, fmt.Errorf("meta: ler resposta: %w", errWithoutDetail(err))
	}

	// ClassifyResponse only sees the ENVELOPE (the HTTP STATUS and
	// error.* at the root level). A 4xx/5xx here means NO number was
	// processed — unlike the partial success below, which only exists
	// INSIDE a 200.
	if metaError := ClassifyResponse(resp.StatusCode, rawResponse); metaError != nil {
		return BlockResult{}, metaError
	}
	return blockResult(rawResponse)
}

// blockResult reads added_users/removed_users (success) and
// failed_users (per-number failure) from the SAME `200` envelope.
//
// There's no ambiguity here between "called BlockUsers" and "called
// UnblockUsers": Meta only fills `added_users` on a POST and
// `removed_users` on a DELETE (never both in the same response), so reading
// both fields unconditionally is safe — whichever one Meta didn't send
// stays an empty list.
func blockResult(raw []byte) (BlockResult, error) {
	var envelope struct {
		BlockUsers struct {
			AddedUsers   []succeededItemMeta `json:"added_users"`
			RemovedUsers []succeededItemMeta `json:"removed_users"`
			FailedUsers  []failedItemMeta    `json:"failed_users"`
		} `json:"block_users"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return BlockResult{}, fmt.Errorf("%w: corpo nao entendido", ErrBlockResponseNotUnderstood)
	}

	succeeded := make([]BlockedUser, 0, len(envelope.BlockUsers.AddedUsers)+len(envelope.BlockUsers.RemovedUsers))
	for _, u := range envelope.BlockUsers.AddedUsers {
		succeeded = append(succeeded, BlockedUser{Phone: u.Input, WaID: u.WaID})
	}
	for _, u := range envelope.BlockUsers.RemovedUsers {
		succeeded = append(succeeded, BlockedUser{Phone: u.Input, WaID: u.WaID})
	}

	failures := make([]BlockFailure, 0, len(envelope.BlockUsers.FailedUsers))
	for _, f := range envelope.BlockUsers.FailedUsers {
		failure := BlockFailure{Phone: f.Input, WaID: f.WaID}
		if len(f.Errors) > 0 {
			failure.Message = f.Errors[0].Message
			failure.MetaCode = tolerantInt(f.Errors[0].Code)
			if f.Errors[0].ErrorData.Details != "" {
				failure.Detail = truncateDetail(f.Errors[0].ErrorData.Details)
			}
		}
		failures = append(failures, failure)
	}

	return BlockResult{Succeeded: succeeded, Failed: failures}, nil
}

// BlockUsers blocks `telefones` (already canonicalized by the caller)
// on the `phoneNumberID` instance. Returns the PER-NUMBER verdict — see
// BlockResult.
func (c *Client) BlockUsers(
	ctx context.Context, phoneNumberID, token string, phones []string,
) (BlockResult, error) {
	return c.callBlockUsers(ctx, http.MethodPost, phoneNumberID, token, phones)
}

// UnblockUsers is BlockUsers's mirror: SAME body, DELETE verb.
func (c *Client) UnblockUsers(
	ctx context.Context, phoneNumberID, token string, phones []string,
) (BlockResult, error) {
	return c.callBlockUsers(ctx, http.MethodDelete, phoneNumberID, token, phones)
}

// BlockedItem is ONE number in the block list (GET) — just the `wa_id`:
// Meta doesn't return the `input`/phone number in this listing, only in
// blocking and unblocking.
type BlockedItem struct {
	WaID string
}

// BlockPage is ONE page of GET /block_users — Meta's RAW cursors,
// for the caller to request the next or the previous one.
type BlockPage struct {
	Items        []BlockedItem
	CursorBefore string
	CursorAfter  string
}

// ListBlocks reads one page of the block list. `limit` <= 0 omits the
// parameter (Meta decides the default); `after`/`before` omitted when "".
func (c *Client) ListBlocks(
	ctx context.Context, phoneNumberID, token string, limit int, after, before string,
) (BlockPage, error) {
	if !PhoneNumberIDValid(phoneNumberID) {
		return BlockPage{}, ErrInvalidPhoneNumberID
	}
	target, err := url.JoinPath(c.base, phoneNumberID, "block_users")
	if err != nil {
		return BlockPage{}, fmt.Errorf("meta: montar url: %w", err)
	}

	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if after != "" {
		q.Set("after", after)
	}
	if before != "" {
		q.Set("before", before)
	}
	if len(q) > 0 {
		target += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return BlockPage{}, fmt.Errorf("meta: montar requisicao: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return BlockPage{}, fmt.Errorf("meta: falha de transporte ao listar bloqueios: %w", errWithoutDetail(err))
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return BlockPage{}, fmt.Errorf("meta: ler resposta: %w", errWithoutDetail(err))
	}
	if metaError := ClassifyResponse(resp.StatusCode, raw); metaError != nil {
		return BlockPage{}, metaError
	}

	var envelope struct {
		Data []struct {
			WaID string `json:"wa_id"`
		} `json:"data"`
		Paging struct {
			Cursors struct {
				After  string `json:"after"`
				Before string `json:"before"`
			} `json:"cursors"`
		} `json:"paging"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return BlockPage{}, fmt.Errorf("%w: corpo nao entendido", ErrBlockResponseNotUnderstood)
	}

	items := make([]BlockedItem, 0, len(envelope.Data))
	for _, d := range envelope.Data {
		items = append(items, BlockedItem{WaID: d.WaID})
	}
	return BlockPage{
		Items:        items,
		CursorBefore: envelope.Paging.Cursors.Before,
		CursorAfter:  envelope.Paging.Cursors.After,
	}, nil
}
