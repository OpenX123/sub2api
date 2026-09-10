package service

import (
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

func TestOpenCodeAccountProbeSessionAndModel(t *testing.T) {
	for _, tc := range []struct {
		name, base, selected, incoming, wantModel string
		wantSession                               bool
	}{
		{"default", "https://opencode.ai/zen/go", "", "", "muse-spark-1.3-contributor", true},
		{"caller session", "https://opencode.ai/zen/go", "alias", "conversation-123", "muse-spark-1.3-contributor", true},
		{"other origin", "https://example.com", "alias", "", "muse-spark-1.3-contributor", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"output\":[]}}\n\n"))}}
			svc := &AccountTestService{httpUpstream: upstream, cfg: &config.Config{}, tlsFPProfileService: &TLSFingerprintProfileService{}}
			account := &Account{ID: 56, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key", "base_url": tc.base, "model_mapping": map[string]any{"alias": "muse-spark-1.3-contributor"}}, Extra: map[string]any{"openai_responses_supported": true}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/api/v1/admin/accounts/56/test", nil)
			c.Request.Header.Set(openCodeSessionHeader, tc.incoming)
			require.NoError(t, svc.testOpenAIAccountConnection(c, account, tc.selected, "", ""))
			require.Equal(t, tc.wantModel, gjson.GetBytes(upstream.lastBody, "model").String())
			if tc.wantSession {
				require.NotEmpty(t, upstream.lastReq.Header.Get(openCodeSessionHeader))
				if tc.incoming != "" {
					require.Equal(t, tc.incoming, upstream.lastReq.Header.Get(openCodeSessionHeader))
				}
			} else {
				require.Empty(t, upstream.lastReq.Header.Get(openCodeSessionHeader))
			}
		})
	}
}
