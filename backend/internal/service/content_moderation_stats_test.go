package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeModerationStatsStore struct {
	mu    sync.Mutex
	total ContentModerationDailyStats
	days  []string
}

func (f *fakeModerationStatsStore) Add(_ context.Context, day string, delta ContentModerationDailyStats) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.days = append(f.days, day)
	f.total = f.total.Plus(delta)
	return nil
}

func (f *fakeModerationStatsStore) Get(_ context.Context, _ string) (ContentModerationDailyStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.total, nil
}

func (f *fakeModerationStatsStore) snapshot() ContentModerationDailyStats {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.total
}

// newOpenAIModerationTestService builds a service whose OpenAI-compatible engine always returns the given sexual score.
func newOpenAIModerationTestService(t *testing.T, sexualScore float64, mutate func(*ContentModerationConfig)) (*ContentModerationService, *contentModerationTestRepo, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": sexualScore}}}})
	}))
	t.Cleanup(server.Close)
	cfg := defaultContentModerationConfig()
	cfg.Enabled, cfg.Mode = true, ContentModerationModePreBlock
	cfg.EmailOnHit, cfg.AutoBanEnabled = false, false
	cfg.BaseURL, cfg.APIKeys = server.URL, []string{"sk-test"}
	if mutate != nil {
		mutate(cfg)
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	repo := &contentModerationTestRepo{}
	svc := &ContentModerationService{
		settingRepo: &contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled: "true", SettingKeyContentModerationConfig: string(raw),
		}},
		repo: repo, hashCache: &contentModerationTestHashCache{hashes: map[string]struct{}{}},
		httpClient: server.Client(), asyncQueue: make(chan contentModerationTask, 16), keyHealth: map[string]*contentModerationKeyHealth{},
	}
	return svc, repo, server
}

func moderationChatBody(text string) []byte {
	raw, _ := json.Marshal(map[string]any{"messages": []map[string]any{{"role": "user", "content": text}}})
	return raw
}

func TestContentModerationStatsCountsBlockedAudit(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.99, nil)
	store := &fakeModerationStatsStore{}
	svc.SetStatsStore(store)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 7, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("bad prompt")})

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationDailyStats{Audits: 1, UpstreamCalls: 1, Flagged: 1, Blocked: 1}, store.snapshot())
	require.Equal(t, []string{ContentModerationStatsDay(time.Now())}, store.days)
}

func TestContentModerationStatsCountsKeywordBlock(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.0, func(cfg *ContentModerationConfig) {
		cfg.BlockedKeywords = []string{"forbidden-word"}
	})
	store := &fakeModerationStatsStore{}
	svc.SetStatsStore(store)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 7, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("forbidden-word here")})

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationDailyStats{Audits: 1, KeywordBlocks: 1}, store.snapshot())
}

func TestContentModerationStatsDayUsesShanghai(t *testing.T) {
	utc := time.Date(2026, 11, 14, 16, 30, 0, 0, time.UTC) // 2026-11-15 00:30 in Shanghai
	require.Equal(t, "20261115", ContentModerationStatsDay(utc))
}
