package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newSafeCacheTestService(t *testing.T, cfg *ContentModerationConfig, scores map[string]float64) (*ContentModerationService, *contentModerationTestRepo, *contentModerationTestHashCache, *atomic.Int64) {
	t.Helper()
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{
			Results: []moderationAPIResult{{CategoryScores: scores}},
		})
	}))
	t.Cleanup(server.Close)

	cfg.Enabled = true
	cfg.BaseURL = server.URL
	cfg.APIKeys = []string{"sk-test"}
	rawCfg, err := json.Marshal(cfg)
	require.NoError(t, err)

	repo := &contentModerationTestRepo{}
	cache := &contentModerationTestHashCache{}
	svc := NewContentModerationService(
		&contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled:      "true",
			SettingKeyContentModerationConfig: string(rawCfg),
		}},
		repo,
		cache,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	return svc, repo, cache, &requests
}

func safeCacheCheck(t *testing.T, svc *ContentModerationService, prompt string) *ContentModerationDecision {
	t.Helper()
	body, err := json.Marshal(map[string]any{"messages": []map[string]string{{"role": "user", "content": prompt}}})
	require.NoError(t, err)
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     body,
	})
	require.NoError(t, err)
	return decision
}

func TestContentModerationSafeCache_RepeatedSafeInputCallsUpstreamOnce(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Mode = ContentModerationModePreBlock
	cfg.RecordNonHits = true
	svc, repo, cache, requests := newSafeCacheTestService(t, cfg, map[string]float64{"harassment": 0.02})

	first := safeCacheCheck(t, svc, "帮我写一首关于秋天的诗")
	second := safeCacheCheck(t, svc, "帮我写一首关于秋天的诗")

	require.True(t, first.Allowed)
	require.True(t, second.Allowed)
	require.Equal(t, first.CategoryScores, second.CategoryScores)
	require.Equal(t, int64(1), requests.Load())
	require.Equal(t, 1, cache.safeSets)
	for _, ttl := range cache.safeTTLs {
		require.Equal(t, time.Duration(defaultContentModerationSafeResultCacheTTLSeconds)*time.Second, ttl)
	}

	logs := requireContentModerationLogCount(t, repo, 2)
	cacheHits := 0
	for _, log := range logs {
		require.Equal(t, ContentModerationActionAllow, log.Action)
		if log.EngineMeta != nil && log.EngineMeta.CacheHit {
			cacheHits++
		}
	}
	require.Equal(t, 1, cacheHits)
}

func TestContentModerationSafeCache_DifferentInputIsAuditedAgain(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Mode = ContentModerationModePreBlock
	svc, _, _, requests := newSafeCacheTestService(t, cfg, map[string]float64{"harassment": 0.02})

	safeCacheCheck(t, svc, "第一段文本")
	safeCacheCheck(t, svc, "第二段文本")

	require.Equal(t, int64(2), requests.Load())
}

func TestContentModerationSafeCache_FlaggedInputIsNeverCached(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Mode = ContentModerationModePreBlock
	svc, _, cache, requests := newSafeCacheTestService(t, cfg, map[string]float64{"sexual": 0.9})

	first := safeCacheCheck(t, svc, "flagged prompt")
	second := safeCacheCheck(t, svc, "flagged prompt")

	require.True(t, first.Blocked)
	require.True(t, second.Blocked)
	require.Equal(t, int64(2), requests.Load())
	require.Zero(t, cache.safeSets)
}

func TestContentModerationSafeCache_DisabledByNegativeTTL(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Mode = ContentModerationModePreBlock
	cfg.SafeResultCacheTTLSeconds = -1
	svc, _, cache, requests := newSafeCacheTestService(t, cfg, map[string]float64{"harassment": 0.02})

	safeCacheCheck(t, svc, "same prompt")
	safeCacheCheck(t, svc, "same prompt")

	require.Equal(t, int64(2), requests.Load())
	require.Zero(t, cache.safeGets)
	require.Zero(t, cache.safeSets)
}

func TestContentModerationSafeCache_CacheErrorFallsBackToUpstream(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Mode = ContentModerationModePreBlock
	svc, _, cache, requests := newSafeCacheTestService(t, cfg, map[string]float64{"harassment": 0.02})
	cache.safeGetErr = errors.New("redis down")

	decision := safeCacheCheck(t, svc, "same prompt")

	require.True(t, decision.Allowed)
	require.Equal(t, int64(1), requests.Load())
}

func TestContentModerationSafeCache_ObserveModeAlsoUsesCache(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Mode = ContentModerationModeObserve
	svc, _, _, requests := newSafeCacheTestService(t, cfg, map[string]float64{"harassment": 0.02})

	safeCacheCheck(t, svc, "async prompt")
	require.Eventually(t, func() bool { return requests.Load() == 1 }, time.Second, 10*time.Millisecond)
	safeCacheCheck(t, svc, "async prompt")
	require.Eventually(t, func() bool { return svc.asyncProcessed.Load() == 2 }, time.Second, 10*time.Millisecond)

	require.Equal(t, int64(1), requests.Load())
}

func TestContentModerationSafeCache_LoweredThresholdIgnoresCachedVerdict(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.Engine = ContentModerationEngineOpenAI
	cfg.Thresholds = ContentModerationDefaultThresholds()
	cfg.Thresholds["harassment"] = 0.9
	cache := &contentModerationTestHashCache{}
	svc := &ContentModerationService{hashCache: cache}
	key := safeResultCacheKey(cfg, "input-hash")
	svc.storeSafeModerationResult(context.Background(), cfg, key, &moderationAPIResult{
		CategoryScores: map[string]float64{"harassment": 0.5},
		EngineMeta:     &ContentModerationEngineMeta{Engine: ContentModerationEngineOpenAI, Model: "omni"},
	})

	hit := svc.lookupSafeModerationResult(context.Background(), cfg, key)
	require.NotNil(t, hit)
	require.True(t, hit.EngineMeta.CacheHit)
	require.Equal(t, "omni", hit.EngineMeta.Model)
	require.False(t, cache.safeResults[key].EngineMeta.CacheHit, "stored entry must not be mutated by a hit")

	cfg.Thresholds["harassment"] = 0.4
	require.Nil(t, svc.lookupSafeModerationResult(context.Background(), cfg, key))
}

func TestSafeResultCacheKey_BindsEngineEndpointModelAndInput(t *testing.T) {
	base := defaultContentModerationConfig()
	base.Engine = ContentModerationEngineTypeSafe
	base.BaseURL = "https://api.typesafe.ai"
	base.Model = "jev-latest"
	key := safeResultCacheKey(base, "hash-a")
	require.NotEmpty(t, key)
	require.Equal(t, key, safeResultCacheKey(base, "hash-a"))

	trailingSlash := cloneContentModerationConfig(base)
	trailingSlash.BaseURL = "https://api.typesafe.ai/"
	require.Equal(t, key, safeResultCacheKey(trailingSlash, "hash-a"))

	threshold := cloneContentModerationConfig(base)
	threshold.Thresholds = map[string]float64{"sexual": 0.1}
	require.Equal(t, key, safeResultCacheKey(threshold, "hash-a"), "thresholds are applied on read, not baked into the key")

	require.NotEqual(t, key, safeResultCacheKey(base, "hash-b"))

	model := cloneContentModerationConfig(base)
	model.Model = "@makers/jev"
	require.NotEqual(t, key, safeResultCacheKey(model, "hash-a"))

	endpoint := cloneContentModerationConfig(base)
	endpoint.BaseURL = "https://ai-gateway.edgeone.link"
	require.NotEqual(t, key, safeResultCacheKey(endpoint, "hash-a"))

	engine := cloneContentModerationConfig(base)
	engine.Engine = ContentModerationEngineOpenAI
	require.NotEqual(t, key, safeResultCacheKey(engine, "hash-a"))

	require.Empty(t, safeResultCacheKey(base, " "))
	require.Empty(t, safeResultCacheKey(nil, "hash-a"))
}

func TestNormalizeSafeResultCacheTTLSeconds(t *testing.T) {
	require.Equal(t, defaultContentModerationSafeResultCacheTTLSeconds, normalizeSafeResultCacheTTLSeconds(0))
	require.Equal(t, -1, normalizeSafeResultCacheTTLSeconds(-30))
	require.Equal(t, 600, normalizeSafeResultCacheTTLSeconds(600))
	require.Equal(t, maxContentModerationSafeResultCacheTTLSeconds, normalizeSafeResultCacheTTLSeconds(10*maxContentModerationSafeResultCacheTTLSeconds))
}

func TestContentModerationUpdateConfig_SavesSafeResultCacheTTL(t *testing.T) {
	settings := &contentModerationTestSettingRepo{values: map[string]string{}}
	svc := NewContentModerationService(settings, &contentModerationTestRepo{}, &contentModerationTestHashCache{}, nil, nil, nil, nil, nil)

	view, err := svc.GetConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, defaultContentModerationSafeResultCacheTTLSeconds, view.SafeResultCacheTTLSeconds)

	disabled := -1
	view, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{SafeResultCacheTTLSeconds: &disabled})
	require.NoError(t, err)
	require.Equal(t, -1, view.SafeResultCacheTTLSeconds)

	// A later save that omits the field (e.g. an older admin UI) keeps the stored value.
	enabled := true
	view, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{RecordNonHits: &enabled})
	require.NoError(t, err)
	require.Equal(t, -1, view.SafeResultCacheTTLSeconds)
}
