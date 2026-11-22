package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIsPlausibleMessagesBetaToken(t *testing.T) {
	now := messagesBetaTestNow
	accepted := []string{
		// Sent by Claude Code 2.1.31x-2.1.329 and rejected by the old allowlist.
		"per-turn-control-2026-07-01",
		"mid-conversation-system-2026-04-07",
		"dangerous-tool-use-2026-09-03",
		"structured-outputs-2025-12-15",
		"afk-mode-2026-01-31",
		"adaptive-thinking-2026-01-28",
		// Compact date and digits in the name.
		claude.BetaClaudeCode,
		"max-tokens-3-5-sonnet-2024-07-15",
		"context-1m-2025-08-07",
		// Inside the future tolerance.
		"brand-new-2027-05-01",
	}
	accepted = append(accepted, claude.FullClaudeCodeMimicryBetas()...)
	for _, token := range accepted {
		require.True(t, isPlausibleMessagesBetaToken(token, now), token)
	}
	for _, token := range []string{
		// Probes seen in production.
		"gfs",
		"vibers-bogus-2099",
		"vibers-nope-2098",
		"zz-probe-2099-01-01",
		"unsupported-beta-probe-2099-01-01",
		// Structurally invalid.
		"2026-01-01",
		"-feature-2026-01-01",
		"feature--2026-01-01",
		"Feature-2026-01-01",
		"feature-2026-1-01",
		"feature-2026-01-01-",
		"feature_2026-01-01",
		"feature 2026-01-01",
		"feature-202601011",
		// Impossible or out-of-range dates.
		"feature-2026-02-30",
		"feature-2026-13-01",
		"feature-20261301",
		"feature-2022-12-31",
		"feature-2027-05-21",
	} {
		require.False(t, isPlausibleMessagesBetaToken(token, now), token)
	}
}

func TestValidateMessagesBetaHeaderDispatch(t *testing.T) {
	fixMessagesBetaNow(t)

	for _, tc := range []struct {
		name    string
		headers []string
		invalid string
	}{
		{name: "no beta"},
		{name: "empty items", headers: []string{" , ,"}},
		{name: "existing Claude Code default", headers: []string{claude.DefaultBetaHeader}},
		{name: "full Claude Code set", headers: []string{strings.Join(claude.FullClaudeCodeMimicryBetas(), ",")}},
		{name: "new Claude Code betas", headers: []string{claude.DefaultBetaHeader + ",per-turn-control-2026-07-01, mid-conversation-system-2026-04-07", "afk-mode-2026-01-31"}},
		{name: "future-dated probe", headers: []string{claude.DefaultBetaHeader + ",zz-probe-2099-01-01"}, invalid: "zz-probe-2099-01-01"},
		{name: "undated probe in second header", headers: []string{claude.DefaultBetaHeader, "gfs"}, invalid: "gfs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			called := false
			router.POST("/v1/messages", validateMessagesBetaHeader, func(c *gin.Context) {
				called = true
				require.Equal(t, tc.headers, c.Request.Header.Values("Anthropic-Beta"))
				c.Status(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			for _, value := range tc.headers {
				req.Header.Add("Anthropic-Beta", value)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if tc.invalid == "" {
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				require.True(t, called)
				return
			}
			require.False(t, called)
			requireAnthropicBetaRejection(t, w, tc.invalid)
		})
	}
}

func TestGatewayRoutesMessagesBetaValidationDoesNotApplyToCountTokens(t *testing.T) {
	router := newGatewayRoutesTestRouter(service.PlatformGrok)
	for _, path := range []string{"/v1/messages/count_tokens", "/messages/count_tokens"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"grok","messages":[{"role":"user","content":"hi"}]}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Anthropic-Beta", "unknown-feature-2099-01-01")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"input_tokens"`)
		})
	}
}

var messagesBetaTestNow = time.Date(2026, time.November, 21, 0, 0, 0, 0, time.UTC)

// fixMessagesBetaNow pins the clock; tests using it must not run in parallel.
func fixMessagesBetaNow(t *testing.T) {
	t.Helper()
	previous := messagesBetaNow
	messagesBetaNow = func() time.Time { return messagesBetaTestNow }
	t.Cleanup(func() { messagesBetaNow = previous })
}

func requireAnthropicBetaRejection(t *testing.T, w *httptest.ResponseRecorder, token string) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var response struct {
		Type  string
		Error struct{ Type, Message string }
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, "error", response.Type)
	require.Equal(t, "invalid_request_error", response.Error.Type)
	require.Equal(t, "Unexpected value(s) `"+token+"` for the `anthropic-beta` header. Please consult our documentation at platform.claude.com/docs or try again without the header.", response.Error.Message)
}
