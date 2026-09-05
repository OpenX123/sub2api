//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestEnsureResponsesReasoningSummary_AddsOnlyMissingReasoningSummaries(t *testing.T) {
	body := []byte(`{"model":"muse-spark-1.3-contributor","input":[
		{"type":"reasoning","encrypted_content":"enc-1"},
		{"type":"reasoning","encrypted_content":"enc-2","summary":null},
		{"type":"reasoning","encrypted_content":"enc-3","summary":[{"type":"summary_text","text":"keep"}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
		{"type":"function_call","call_id":"call-1","name":"tool","arguments":"{}"},
		{"type":"function_call_output","call_id":"call-1","output":"ok"}
	]}`)

	updated, changed, err := ensureResponsesReasoningSummary(body)
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{"model":"muse-spark-1.3-contributor","input":[
		{"type":"reasoning","encrypted_content":"enc-1","summary":[]},
		{"type":"reasoning","encrypted_content":"enc-2","summary":[]},
		{"type":"reasoning","encrypted_content":"enc-3","summary":[{"type":"summary_text","text":"keep"}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},
		{"type":"function_call","call_id":"call-1","name":"tool","arguments":"{}"},
		{"type":"function_call_output","call_id":"call-1","output":"ok"}
	]}`, string(updated))

	repeated, repeatedChanged, err := ensureResponsesReasoningSummary(updated)
	require.NoError(t, err)
	require.False(t, repeatedChanged)
	require.Equal(t, string(updated), string(repeated))
}

func TestEnsureResponsesReasoningSummary_HandlesSingleObjectInput(t *testing.T) {
	updated, changed, err := ensureResponsesReasoningSummary([]byte(`{"input":{"type":"reasoning","encrypted_content":"enc"}}`))
	require.NoError(t, err)
	require.True(t, changed)
	require.JSONEq(t, `{"input":{"type":"reasoning","encrypted_content":"enc","summary":[]}}`, string(updated))

	unchanged, changed, err := ensureResponsesReasoningSummary([]byte(`{"input":"hello"}`))
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, `{"input":"hello"}`, string(unchanged))
}

func TestEnsureResponsesReasoningSummary_DoesNotAddToNonReasoningItems(t *testing.T) {
	body := []byte(`{"input":[{"type":"message"},{"type":"function_call","summary":null},{"type":"function_call_output"}]}`)
	updated, changed, err := ensureResponsesReasoningSummary(body)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, string(body), string(updated))

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(updated, &decoded))
	require.Len(t, decoded["input"], 3)
}

type strictResponsesSummaryUpstream struct {
	httpUpstreamRecorder
	missingSummary bool
	forceStream    bool
}

func (u *strictResponsesSummaryUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	_, _ = u.httpUpstreamRecorder.Do(req, proxyURL, accountID, accountConcurrency)
	u.missingSummary = requestHasMissingReasoningSummary(u.lastBody)
	if u.missingSummary {
		return newJSONResponse(http.StatusBadRequest, `{"error":{"type":"invalid_request_error","message":"input[0] missing required field summary"}}`), nil
	}
	return u.successResponse(), nil
}

func (u *strictResponsesSummaryUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func (u *strictResponsesSummaryUpstream) successResponse() *http.Response {
	if u.forceStream || strings.Contains(u.lastReq.Header.Get("Accept"), "text/event-stream") || gjson.GetBytes(u.lastBody, "stream").Bool() {
		body := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_summary\",\"object\":\"response\",\"model\":\"muse-spark-1.3-contributor\",\"status\":\"in_progress\",\"output\":[]}}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_summary\",\"object\":\"response\",\"model\":\"muse-spark-1.3-contributor\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\ndata: [DONE]\n\n"
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	}
	return newJSONResponse(http.StatusOK, `{"id":"resp_summary","object":"response","model":"muse-spark-1.3-contributor","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)
}

func requestHasMissingReasoningSummary(body []byte) bool {
	input := gjson.GetBytes(body, "input")
	check := func(item gjson.Result) bool {
		return item.IsObject() && item.Get("type").String() == "reasoning" &&
			(!item.Get("summary").Exists() || item.Get("summary").Type == gjson.Null)
	}
	if input.IsArray() {
		missing := false
		input.ForEach(func(_, item gjson.Result) bool {
			missing = check(item)
			return !missing
		})
		return missing
	}
	if input.IsObject() {
		return check(input)
	}
	return false
}

func TestForwardAsAnthropic_APIKeyResponsesIncludesReasoningSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	body := []byte(`{"model":"claude-opus-5","max_tokens":16,"messages":[
		{"role":"user","content":"plan this"},
		{"role":"assistant","content":[{"type":"thinking","thinking":"plan","signature":"enc-signature"},{"type":"text","text":"done"}]}
	],"stream":false}`)
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &strictResponsesSummaryUpstream{}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          57,
		Name:        "muse-api-key",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://compat.example/v1"},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
	}

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "muse-spark-1.3-contributor")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, upstream.missingSummary)
	require.Equal(t, "summary_text", gjson.GetBytes(upstream.lastBody, "input.1.summary.0.type").String())
	require.Equal(t, "plan", gjson.GetBytes(upstream.lastBody, "input.1.summary.0.text").String())
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestForwardAsAnthropic_OAuthResponsesIncludesReasoningSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	body := []byte(`{"model":"claude-opus-5","max_tokens":16,"messages":[
		{"role":"user","content":"continue"},
		{"role":"assistant","content":[{"type":"thinking","thinking":"plan","signature":"enc-signature"},{"type":"text","text":"done"}]}
	],"stream":false}`)
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &strictResponsesSummaryUpstream{forceStream: true}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          58,
		Name:        "muse-oauth",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token":       "oauth-token",
			"chatgpt_account_id": "chatgpt-acc",
		},
	}

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "muse-spark-1.3-contributor")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, upstream.missingSummary)
	require.Equal(t, "summary_text", gjson.GetBytes(upstream.lastBody, "input.1.summary.0.type").String())
	require.Equal(t, "plan", gjson.GetBytes(upstream.lastBody, "input.1.summary.0.text").String())
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestForward_APIKeyResponsesFallbackIncludesMissingReasoningSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	body := []byte(`{"model":"muse-spark-1.3-contributor","stream":false,"input":[{"type":"reasoning","encrypted_content":"enc"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &strictResponsesSummaryUpstream{}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
		httpUpstream: upstream,
	}
	account := &Account{
		ID:          57,
		Name:        "muse-api-key",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://compat.example/v1"},
		Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
	}

	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, upstream.missingSummary)
	require.Equal(t, "[]", gjson.GetBytes(upstream.lastBody, "input.0.summary").Raw)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestForwardAsChatCompletions_ResponsesShapeAddsReasoningSummary(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
	}{
		{
			name: "api key",
			account: &Account{
				ID:          57,
				Name:        "muse-api-key",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-test"},
				Extra:       map[string]any{openai_compat.ExtraKeyResponsesSupported: true},
			},
		},
		{
			name: "oauth",
			account: &Account{
				ID:          58,
				Name:        "muse-oauth",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				Concurrency: 1,
				Credentials: map[string]any{
					"access_token":       "oauth-token",
					"chatgpt_account_id": "chatgpt-acc",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			rec := httptest.NewRecorder()
			body := []byte(`{"model":"muse-spark-1.3-contributor","stream":false,"input":[{"type":"reasoning","encrypted_content":"enc"},{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}]}`)
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")

			upstream := &strictResponsesSummaryUpstream{forceStream: true}
			svc := &OpenAIGatewayService{
				cfg:          &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}},
				httpUpstream: upstream,
			}

			result, err := svc.ForwardAsChatCompletions(context.Background(), c, tt.account, body, "", "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.False(t, upstream.missingSummary)
			require.Equal(t, "[]", gjson.GetBytes(upstream.lastBody, "input.0.summary").Raw)
			require.Equal(t, http.StatusOK, rec.Code)
		})
	}
}
