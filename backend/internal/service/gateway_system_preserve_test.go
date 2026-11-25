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

func TestRewriteSystemKeepsInstructionsAfterClaudeCodeIdentity(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"暗号是什么"}]}`)
	tests := []struct {
		name       string
		system     any
		wantSecret string
	}{
		{"same string", "You are Claude Code, Anthropic's official CLI for Claude. 暗号是 BLUE-42", "暗号是 BLUE-42"},
		{"next line", "You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.\n暗号是 BLUE-42", "暗号是 BLUE-42"},
		{"separate blocks with client billing block", []any{
			map[string]any{"type": "text", "text": "x-anthropic-billing-header: cc_version=2.1.0.abc; cc_entrypoint=cli;"},
			map[string]any{"type": "text", "text": "You are Claude Code, Anthropic's official CLI for Claude."},
			map[string]any{"type": "text", "text": "暗号是 BLUE-42"},
		}, "暗号是 BLUE-42"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := rewriteSystemForNonClaudeCodeWithPromptBlocks(body, tt.system, "", "")
			msgs := gjson.GetBytes(out, "messages").Array()
			require.Len(t, msgs, 3)
			text := msgs[0].Get("content.0.text").String()
			require.Equal(t, "[System Instructions]\n"+tt.wantSecret, text)
			require.NotContains(t, gjson.GetBytes(out, "messages").Raw, "x-anthropic-billing-header")
		})
	}

	// 只有身份句时没有需要保留的内容，不注入消息对。
	out := rewriteSystemForNonClaudeCodeWithPromptBlocks(body, "You are Claude Code, Anthropic's official CLI for Claude.", "", "")
	require.Len(t, gjson.GetBytes(out, "messages").Array(), 1)
}

// count_tokens 必须和 /v1/messages 发出同样的 system/messages，否则两边的 token 数对不上。
func TestForwardCountTokensMatchesMessagesBodyOnMimicPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	reqBody := `{"model":"claude-sonnet-4-6","max_tokens":64,` +
		`"system":[{"type":"text","text":"You are Claude Code, Anthropic's official CLI for Claude.","cache_control":{"type":"ephemeral"}},{"type":"text","text":"暗号是 BLUE-42"}],` +
		`"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}]}`
	account := &Account{
		ID: 402, Name: "mimic-consistency", Platform: PlatformAnthropic, Type: AccountTypeSetupToken,
		Concurrency: 1, Credentials: map[string]any{"access_token": "tok"}, Status: StatusActive, Schedulable: true,
	}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}}

	send := func(path, respBody string, call func(*GatewayService, *gin.Context, *ParsedRequest) error) []byte {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, path, nil)
		c.Request.Header.Set("User-Agent", "curl/8.0")
		parsed, err := ParseGatewayRequest(NewRequestBodyRef([]byte(reqBody)), PlatformAnthropic)
		require.NoError(t, err)
		upstream := &anthropicHTTPUpstreamRecorder{resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(respBody)),
		}}
		svc := &GatewayService{
			cfg: cfg, responseHeaderFilter: compileResponseHeaderFilter(cfg), httpUpstream: upstream,
			rateLimitService: &RateLimitService{}, deferredService: &DeferredService{},
		}
		require.NoError(t, call(svc, c, parsed))
		return upstream.lastBody
	}

	messagesBody := send("/v1/messages",
		`{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`,
		func(s *GatewayService, c *gin.Context, p *ParsedRequest) error {
			_, err := s.Forward(context.Background(), c, account, p)
			return err
		})
	countBody := send("/v1/messages/count_tokens", `{"input_tokens":1}`,
		func(s *GatewayService, c *gin.Context, p *ParsedRequest) error {
			return s.ForwardCountTokens(context.Background(), c, account, p)
		})

	require.Equal(t, gjson.GetBytes(messagesBody, "system").Raw, gjson.GetBytes(countBody, "system").Raw)
	require.Equal(t, gjson.GetBytes(messagesBody, "messages").Raw, gjson.GetBytes(countBody, "messages").Raw)
	require.Contains(t, gjson.GetBytes(countBody, "messages").Raw, "BLUE-42")
}
