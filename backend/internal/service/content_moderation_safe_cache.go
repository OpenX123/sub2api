package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// typeSafeModerationQuestionsDigest fingerprints the exact TypeSafe question
// set, so editing a rule invalidates cached verdicts even if nobody bumps
// TypeSafeModerationRulesVersion.
var typeSafeModerationQuestionsDigest = sync.OnceValue(func() string {
	raw, err := json.Marshal(typeSafeModerationQuestions())
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
})

func normalizeSafeResultCacheTTLSeconds(seconds int) int {
	switch {
	case seconds < 0:
		return -1
	case seconds == 0:
		return defaultContentModerationSafeResultCacheTTLSeconds
	case seconds > maxContentModerationSafeResultCacheTTLSeconds:
		return maxContentModerationSafeResultCacheTTLSeconds
	default:
		return seconds
	}
}

func safeResultCacheTTL(cfg *ContentModerationConfig) time.Duration {
	if cfg == nil {
		return 0
	}
	seconds := normalizeSafeResultCacheTTLSeconds(cfg.SafeResultCacheTTLSeconds)
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

// safeResultCacheKey binds a verdict to everything that can change it besides
// thresholds: the engine, its endpoint and model, the TypeSafe rule set, and
// the normalized input (text plus image hashes).
func safeResultCacheKey(cfg *ContentModerationConfig, inputHash string) string {
	inputHash = strings.TrimSpace(inputHash)
	if cfg == nil || inputHash == "" {
		return ""
	}
	engine := moderationEngine(cfg.Engine)
	rules := ""
	if engine == ContentModerationEngineTypeSafe {
		digest := typeSafeModerationQuestionsDigest()
		if digest == "" {
			return ""
		}
		rules = TypeSafeModerationRulesVersion + ":" + digest
	}
	h := sha256.New()
	for _, part := range []string{
		"safe-result:v1",
		engine,
		strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		strings.TrimSpace(cfg.Model),
		rules,
		inputHash,
	} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// lookupSafeModerationResult returns a cached verdict only if it is still below
// every current threshold; anything else falls through to a real audit.
func (s *ContentModerationService) lookupSafeModerationResult(ctx context.Context, cfg *ContentModerationConfig, key string) *moderationAPIResult {
	if s == nil || s.hashCache == nil || key == "" || safeResultCacheTTL(cfg) == 0 {
		return nil
	}
	cached, err := s.hashCache.GetSafeModerationResult(ctx, key)
	if err != nil {
		slog.Warn("content_moderation.safe_cache_get_failed", "error", err)
		return nil
	}
	if cached == nil || len(cached.CategoryScores) == 0 {
		return nil
	}
	if flagged, _, _ := evaluateModerationScores(cached.CategoryScores, cfg.Thresholds); flagged {
		return nil
	}
	meta := &ContentModerationEngineMeta{Engine: moderationEngine(cfg.Engine)}
	if cached.EngineMeta != nil {
		copied := *cached.EngineMeta
		meta = &copied
	}
	meta.CacheHit = true
	return &moderationAPIResult{CategoryScores: cloneFloatMap(cached.CategoryScores), EngineMeta: meta}
}

func (s *ContentModerationService) storeSafeModerationResult(ctx context.Context, cfg *ContentModerationConfig, key string, result *moderationAPIResult) {
	ttl := safeResultCacheTTL(cfg)
	if s == nil || s.hashCache == nil || key == "" || ttl == 0 || result == nil || len(result.CategoryScores) == 0 {
		return
	}
	var meta *ContentModerationEngineMeta
	if result.EngineMeta != nil {
		copied := *result.EngineMeta
		copied.CacheHit = false
		meta = &copied
	}
	entry := &ContentModerationCachedResult{CategoryScores: cloneFloatMap(result.CategoryScores), EngineMeta: meta}
	if err := s.hashCache.SetSafeModerationResult(ctx, key, entry, ttl); err != nil {
		slog.Warn("content_moderation.safe_cache_set_failed", "error", err)
	}
}
