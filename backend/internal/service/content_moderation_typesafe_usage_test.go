package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// typeSafeAnswersBody builds a successful System One response covering all 13 categories.
func typeSafeAnswersBody(t *testing.T, scores map[string]float64, inputTokens int) []byte {
	t.Helper()
	answers := map[string]any{}
	for _, category := range ContentModerationCategories() {
		answers[category] = map[string]any{"type": "noul", "noul": scores[category]}
	}
	raw, err := json.Marshal(map[string]any{
		"model":   "jev-1.14.0",
		"answers": answers,
		"usage":   map[string]any{"input_tokens": inputTokens, "output_tokens": 13},
	})
	require.NoError(t, err)
	return raw
}

func newTypeSafeTestConfig(baseURL string) *ContentModerationConfig {
	cfg := defaultContentModerationConfig()
	cfg.Engine = ContentModerationEngineTypeSafe
	cfg.BaseURL, cfg.Model = baseURL, "jev-1.14.0"
	cfg.APIKeys = []string{"ts-test-key"}
	cfg.TimeoutMS, cfg.RetryCount = 2000, 2
	return cfg
}

func TestTypeSafeModerationRecordsInputTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(typeSafeAnswersBody(t, map[string]float64{"sexual": 0.1}, 1234))
	}))
	defer server.Close()
	svc := &ContentModerationService{httpClient: server.Client(), keyHealth: map[string]*contentModerationKeyHealth{}}

	result, err := svc.callModerationOnceWithInput(context.Background(), newTypeSafeTestConfig(server.URL), "ts-test-key", "需要审核的文字", nil)

	require.NoError(t, err)
	require.Equal(t, 1234, result.EngineMeta.InputTokens)
	require.False(t, result.EngineMeta.Cached)
	raw, err := json.Marshal(result.EngineMeta)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"input_tokens":1234`)
	require.NotContains(t, string(raw), `"cached"`)
}
