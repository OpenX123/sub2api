//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// count_tokens 的上游客户端类错误（未知字段、未知 beta 等）必须原样透传 Anthropic 原文，
// 账号级错误（实名、401/403）仍包装成泛化文案。两条转发路径都要覆盖。
func TestForwardCountTokens_ClientErrorPassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)

	unknownField := `{"type":"error","error":{"type":"invalid_request_error","message":"foo: Extra inputs are not permitted"},"request_id":"req_x"}`
	kyc := `{"type":"error","error":{"type":"invalid_request_error","message":"Identity verification is required to continue."}}`
	unauthorized := `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`

	tests := []struct {
		name        string
		status      int
		body        string
		wantRaw     bool
		wantMessage string
	}{
		{name: "unknown field passes through", status: 400, body: unknownField, wantRaw: true},
		{name: "identity verification stays wrapped", status: 400, body: kyc, wantMessage: "Upstream request failed"},
		{name: "401 stays wrapped", status: 401, body: unauthorized, wantMessage: "Upstream request failed"},
	}

	for _, passthrough := range []bool{true, false} {
		for _, tt := range tests {
			name := tt.name
			if passthrough {
				name = "passthrough/" + name
			} else {
				name = "standard/" + name
			}
			t.Run(name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens", nil)

				body := []byte(`{"model":"claude-sonnet-4-5-20250929","messages":[{"role":"user","content":"hi"}],"foo":1}`)
				parsed := &ParsedRequest{Body: NewRequestBodyRef(body), Model: "claude-sonnet-4-5-20250929"}

				upstream := &anthropicHTTPUpstreamRecorder{
					resp: &http.Response{
						StatusCode: tt.status,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(tt.body)),
					},
				}
				svc := &GatewayService{
					cfg:              &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}},
					httpUpstream:     upstream,
					rateLimitService: &RateLimitService{accountRepo: newSparkShadowRepoStub()},
					settingService:   NewSettingService(&settingRepoStub{}, &config.Config{}),
				}
				extra := map[string]any{}
				if passthrough {
					extra["anthropic_passthrough"] = true
				}
				account := &Account{
					ID:          401,
					Name:        "ct-acc",
					Platform:    PlatformAnthropic,
					Type:        AccountTypeAPIKey,
					Concurrency: 1,
					Credentials: map[string]any{"api_key": "sk-up", "base_url": "https://api.anthropic.com"},
					Extra:       extra,
					Status:      StatusActive,
					Schedulable: true,
				}

				err := svc.ForwardCountTokens(context.Background(), c, account, parsed)
				require.Error(t, err)
				require.Equal(t, tt.status, rec.Code)
				if tt.wantRaw {
					require.JSONEq(t, tt.body, rec.Body.String())
					return
				}
				require.Equal(t, tt.wantMessage, gjson.Get(rec.Body.String(), "error.message").String())
			})
		}
	}
}
