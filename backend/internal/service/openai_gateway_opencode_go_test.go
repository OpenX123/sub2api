package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openCodeGoHTTPUpstream struct {
	request *http.Request
	body    string
	profile *tlsfingerprint.Profile
}

func (u *openCodeGoHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.request = req
	requestBody, _ := io.ReadAll(req.Body)
	u.body = string(requestBody)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"msg_1","model":"qwen3.8-max","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":3,"output_tokens":2}}`,
		)),
	}, nil
}

func (u *openCodeGoHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.profile = profile
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestForwardAsAnthropicOpenCodeGoUsesNativeMessages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &openCodeGoHTTPUpstream{}
	svc := &OpenAIGatewayService{
		httpUpstream: upstream,
		cfg: &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{
			Enabled: false,
		}}},
	}
	account := &Account{
		ID:          7,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "go-secret",
			"base_url": "https://opencode.ai/zen/go",
		},
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	body := []byte(`{"model":"qwen3.8-max","messages":[{"role":"user","content":"hi"}],"max_tokens":8}`)

	result, err := svc.ForwardAsAnthropic(context.Background(), c, account, body, "", "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "/zen/go/v1/messages", upstream.request.URL.Path)
	require.Equal(t, "go-secret", upstream.request.Header.Get("x-api-key"))
	require.Empty(t, upstream.request.Header.Get("Authorization"))
	require.Equal(t, "OpenCode Go (Node.js 24.x)", upstream.profile.Name)
	require.JSONEq(t, string(body), upstream.body)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 2, result.Usage.OutputTokens)
	require.Equal(t, "/v1/messages", result.UpstreamEndpoint)
	require.Contains(t, recorder.Body.String(), `"text":"ok"`)
}

func TestOpenCodeGoRegionErrorDoesNotDisableWholeAccount(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "go-secret",
			"base_url": "https://opencode.ai/zen/go",
		},
	}
	body := []byte(`{"type":"error","error":{"type":"RegionError","message":"This model is not available in your region."}}`)

	require.True(t, isOpenCodeGoModelAccessError(account, http.StatusForbidden, body))
	require.False(t, (&OpenAIGatewayService{}).handleOpenAIAccountUpstreamError(context.Background(), account, http.StatusForbidden, nil, body, "gpt-5.6-luna"))
}
