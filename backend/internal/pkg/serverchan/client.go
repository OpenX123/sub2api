// Package serverchan sends WeChat notifications through ServerChan Turbo (sct.ftqq.com).
package serverchan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const maxTitleRunes = 32

var (
	ErrInvalidSendKey = errors.New("serverchan: invalid send key")
	sendKeyPattern    = regexp.MustCompile(`^SCT[0-9A-Za-z]+$`)
)

func ValidSendKey(key string) bool { return sendKeyPattern.MatchString(key) }

// TruncateTitle removes line breaks (ServerChan rejects them) and keeps at most 32 runes.
func TruncateTitle(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	runes := []rune(title)
	if len(runes) > maxTitleRunes {
		runes = runes[:maxTitleRunes]
	}
	return string(runes)
}

type Client struct {
	HTTP        *http.Client
	EndpointFor func(sendKey string) string
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{HTTP: httpClient, EndpointFor: func(key string) string {
		return "https://sctapi.ftqq.com/" + key + ".send"
	}}
}

func (c *Client) Send(ctx context.Context, sendKey, title, desp string) error {
	if !ValidSendKey(sendKey) {
		return ErrInvalidSendKey
	}
	form := url.Values{"title": {TruncateTitle(title)}, "desp": {desp}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.EndpointFor(sendKey), strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("serverchan: build request failed")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// Transport errors embed the URL (which contains the key); never return them verbatim.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("serverchan: transport error")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("serverchan: http status %d", resp.StatusCode)
	}
	var out struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return errors.New("serverchan: invalid response")
	}
	if out.Code != 0 {
		return fmt.Errorf("serverchan: api code %d", out.Code)
	}
	return nil
}
