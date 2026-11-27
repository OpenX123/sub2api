package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/feishu"
	"github.com/stretchr/testify/require"
)

func TestDigestAnalysisKeepsUserDetailsLocal(t *testing.T) {
	card := &feishu.DigestCard{Risk: "🛡️ Jev　3 人命中 · 1 人重复拦截", Spend: "今日消费　$91.20 · #99887 最高 $15", RiskRows: []string{"private@example.org"}, Highlight: "用户 #99887 已自动封禁", ErrorRows: []string{"private@example.org"}}
	m := digestAnalysisMetrics(card)
	require.Equal(t, "91.20", m["今日总消费美元"])
	require.Equal(t, "3", m["三小时命中人数"])
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "99887")
	require.NotContains(t, string(raw), "private@example.org")
}
func TestDigestSuggestionsRequireStructuredShortOutput(t *testing.T) {
	require.Equal(t, []string{"观察一", "观察二"}, parseDigestSuggestions(`["观察一","观察二","多余"]`))
	require.Empty(t, parseDigestSuggestions("not JSON"))
}

type digestMiniMaxPool struct {
	AccountRepository
	accounts []Account
}

func (p digestMiniMaxPool) ListSchedulableByPlatform(context.Context, string) ([]Account, error) {
	return p.accounts, nil
}

type digestMiniMaxUpstream struct {
	HTTPUpstream
	ids []int64
}

func (u *digestMiniMaxUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	u.ids = append(u.ids, id)
	if len(u.ids) == 1 {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"content":[{"type":"thinking","thinking":"omit"},{"type":"text","text":"[\"第二个账号成功\"]"}]}`))}, nil
}

func TestDigestAnalysisMiniMaxProtocolsAndFailover(t *testing.T) {
	a := Account{ID: 1, Type: AccountTypeAPIKey, Platform: PlatformMiniMax, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "test-only", "api_protocol": APIProtocolAnthropic, "base_url": "https://api.minimaxi.com/anthropic", "model_mapping": map[string]any{digestAnalysisModel: "actual-model"}}}
	next := a
	next.ID = 2
	transport := &digestMiniMaxUpstream{}
	s := &OpsService{accountRepo: digestMiniMaxPool{accounts: []Account{a, next}}, concurrencyService: &ConcurrencyService{}, openAIGatewayService: &OpenAIGatewayService{httpUpstream: transport}}
	card := &feishu.DigestCard{}
	req, err := s.digestModelRequest(context.Background(), &a, card)
	require.NoError(t, err)
	require.Equal(t, "/anthropic/v1/messages", req.URL.Path)
	require.Equal(t, "test-only", req.Header.Get("x-api-key"))
	require.Equal(t, "2023-06-01", req.Header.Get("anthropic-version"))
	var payload map[string]any
	require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
	require.Equal(t, "actual-model", payload["model"])
	require.Contains(t, payload, "system")
	require.NotContains(t, payload, "reasoning_split")
	require.Equal(t, []string{"第二个账号成功"}, s.digestAnalysis(context.Background(), card))
	require.Equal(t, []int64{1, 2}, transport.ids)
	a.Credentials["api_protocol"] = APIProtocolChatCompletions
	a.Credentials["base_url"] = "https://api.minimaxi.com/v1"
	req, err = s.digestModelRequest(context.Background(), &a, card)
	require.NoError(t, err)
	require.Equal(t, "/v1/chat/completions", req.URL.Path)
	require.Equal(t, "Bearer test-only", req.Header.Get("Authorization"))
	require.Equal(t, []string{"ok"}, readDigestAnalysis(strings.NewReader(`{"choices":[{"message":{"content":"[\"ok\"]"}}]}`), false))
}
