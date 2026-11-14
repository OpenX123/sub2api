package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memoryVerdictCache struct {
	mu     sync.Mutex
	values map[string]map[string]float64
	ttls   map[string]time.Duration
}

func (m *memoryVerdictCache) Get(_ context.Context, key string) (map[string]float64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[key]
	return v, ok, nil
}

func (m *memoryVerdictCache) Set(_ context.Context, key string, scores map[string]float64, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values, m.ttls = map[string]map[string]float64{}, map[string]time.Duration{}
	}
	m.values[key], m.ttls[key] = scores, ttl
	return nil
}

func newCountingModerationService(t *testing.T, statuses []int, mutate func(*ContentModerationConfig)) (*ContentModerationService, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1)) - 1
		if n < len(statuses) && statuses[n] != http.StatusOK {
			w.WriteHeader(statuses[n])
			return
		}
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": 0.1}}}})
	}))
	t.Cleanup(server.Close)
	cfg := defaultContentModerationConfig()
	cfg.Enabled, cfg.Mode, cfg.RecordNonHits = true, ContentModerationModePreBlock, true
	cfg.EmailOnHit, cfg.AutoBanEnabled = false, false
	cfg.BaseURL, cfg.APIKeys, cfg.RetryCount = server.URL, []string{"sk-test"}, 0
	if mutate != nil {
		mutate(cfg)
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	svc := &ContentModerationService{
		settingRepo: &contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "true", SettingKeyContentModerationConfig: string(raw)}},
		repo:        &contentModerationTestRepo{}, hashCache: &contentModerationTestHashCache{hashes: map[string]struct{}{}},
		httpClient: server.Client(), asyncQueue: make(chan contentModerationTask, 16), keyHealth: map[string]*contentModerationKeyHealth{},
	}
	return svc, &calls
}

func checkAndPersist(t *testing.T, svc *ContentModerationService, text string) *ContentModerationLog {
	t.Helper()
	_, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 5, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody(text)})
	require.NoError(t, err)
	task := <-svc.asyncQueue
	return task.log
}

func TestContentModerationVerdictCacheServesRepeatsWithoutCallingEngine(t *testing.T) {
	svc, calls := newCountingModerationService(t, nil, nil)
	cache, store := &memoryVerdictCache{}, &fakeModerationStatsStore{}
	svc.SetVerdictCache(cache)
	svc.SetStatsStore(store)

	first := checkAndPersist(t, svc, "hello")
	second := checkAndPersist(t, svc, "hello")

	require.Equal(t, int32(1), calls.Load())
	require.False(t, first.EngineMeta.Cached)
	require.True(t, second.EngineMeta.Cached)
	require.Equal(t, 0, *second.UpstreamLatencyMS)
	require.Equal(t, ContentModerationDailyStats{Audits: 2, UpstreamCalls: 1, CacheHits: 1}, store.snapshot())
	for _, ttl := range cache.ttls {
		require.Equal(t, 24*time.Hour, ttl)
	}
}

func TestContentModerationVerdictCacheDisabledWithZeroTTL(t *testing.T) {
	svc, calls := newCountingModerationService(t, nil, func(cfg *ContentModerationConfig) { cfg.VerdictCacheMinutes = 0 })
	svc.SetVerdictCache(&memoryVerdictCache{})
	checkAndPersist(t, svc, "hello")
	checkAndPersist(t, svc, "hello")
	require.Equal(t, int32(2), calls.Load())
}

func TestContentModerationVerdictCacheNeverStoresFailures(t *testing.T) {
	svc, calls := newCountingModerationService(t, []int{http.StatusBadRequest}, nil)
	cache := &memoryVerdictCache{}
	svc.SetVerdictCache(cache)
	// engine errors are written straight to the repository, not through the async queue
	_, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 5, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("hello")})
	require.NoError(t, err)
	logs := svc.repo.(*contentModerationTestRepo).snapshotLogs()
	require.Len(t, logs, 1)
	require.NotEmpty(t, logs[0].Error)
	require.Empty(t, cache.values)
	ok := checkAndPersist(t, svc, "hello")
	require.Empty(t, ok.Error)
	require.Equal(t, int32(2), calls.Load())
}

func TestContentModerationVerdictCacheKeyChangesWithModelAndRules(t *testing.T) {
	cfg := defaultContentModerationConfig()
	base := contentModerationVerdictCacheKey(cfg, "h")
	cfg.Model = "omni-moderation-2"
	require.NotEqual(t, base, contentModerationVerdictCacheKey(cfg, "h"))
	ts := newTypeSafeTestConfig("https://typesafe.example")
	require.NotEqual(t, contentModerationVerdictCacheKey(ts, "h"), base)
	require.Len(t, base, 64)
}

func TestContentModerationOptimalDefaults(t *testing.T) {
	cfg := defaultContentModerationConfig()
	require.True(t, cfg.PreHashCheckEnabled)
	require.Equal(t, 1440, cfg.VerdictCacheMinutes)
	require.Equal(t, "jev-1.14.0", moderationEngineDefaults(ContentModerationEngineTypeSafe).Model)

	parsed, err := parseContentModerationConfig(`{"enabled":true,"pre_hash_check_enabled":false}`)
	require.NoError(t, err)
	require.False(t, parsed.PreHashCheckEnabled, "explicit stored values win")
	require.Equal(t, 1440, parsed.VerdictCacheMinutes, "missing field keeps the default")
}
