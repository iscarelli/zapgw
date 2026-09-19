// Package alert delivers one short line of text to the gateway's operator
// when something on the delivery path needs a PERSON — never anything that
// changes what Meta receives (see internal/inbound/mirror.go for the
// criterion this package reacts to: Verdict.Alarm).
//
// WHY THIS EXISTS: between 2026-09-01 and 09-19 a consumer answered ~650
// times with a transient failure and lost 6 messages for good on 09-15, and
// NOBODY found out for 18 days — the gateway only ever wrote "ALARME" to the
// journal and to the per-instance counters, and nothing read either one.
// T-253 adds a channel; it does not change what already decides an event
// needs a person (that stays in mirror.go).
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Notifier sends one line of text to wherever the operator watches it.
// Implementations MUST return promptly (the caller — Sender — bounds this
// with its own timeout regardless) and MUST NOT let a secret reach the
// returned error's message.
type Notifier interface {
	Notify(ctx context.Context, text string) error
}

const defaultTelegramBaseURL = "https://api.telegram.org"

// Telegram is a Notifier over the Bot API's sendMessage endpoint.
//
// THE TOKEN IS A SECRET, THE SAME AS ANY OTHER IN THIS PROJECT (CLAUDE.md,
// "Secrets — never in Git"): it never appears in a log line or in an error
// string. net/http's transport errors typically come back as *url.Error,
// whose Error() carries the FULL REQUEST URL — and the request URL built
// here is exactly BaseURL + "/bot" + Token + "/sendMessage". redactToken
// strips it before any error leaves this method; see the token-leak test in
// telegram_test.go, which proves it against a REAL transport error, not a
// synthetic one.
type Telegram struct {
	Token  string
	ChatID string
	// Client defaults to an *http.Client with a 10s timeout when nil.
	Client *http.Client
	// BaseURL defaults to https://api.telegram.org when empty. A test
	// overrides it to point at an httptest.Server; production never does.
	BaseURL string
}

type sendMessageRequest struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

// Notify posts text as a Telegram message to ChatID. A non-2xx response, a
// transport failure, or a context deadline all come back as a non-nil
// error — never as a partial success.
func (tg *Telegram) Notify(ctx context.Context, text string) error {
	base := tg.BaseURL
	if base == "" {
		base = defaultTelegramBaseURL
	}
	client := tg.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	body, err := json.Marshal(sendMessageRequest{
		ChatID:                tg.ChatID,
		Text:                  text,
		DisableWebPagePreview: true,
	})
	if err != nil {
		// The request was never built, so the URL — and the token inside
		// it — never touched this error.
		return err
	}

	url := base + "/bot" + tg.Token + "/sendMessage"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return redactToken(err, tg.Token)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return redactToken(err, tg.Token)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Built from the status code alone — never from the response body,
		// which Telegram could echo the request back into.
		return fmt.Errorf("telegram sendMessage: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// redactToken removes the token from err's message before it can reach a
// log line. It operates on the STRING, not on a structured field, because
// the leak vector (*url.Error.Error()) is itself a formatted string with no
// structured field to redact instead.
func redactToken(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	msg := strings.ReplaceAll(err.Error(), token, "<redacted>")
	return errors.New(msg)
}
