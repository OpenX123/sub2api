package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"time"
)

const (
	defaultContentModerationVerdictCacheMinutes = 1440
	maxContentModerationVerdictCacheMinutes     = 10080
	contentModerationVerdictCacheTimeout        = 100 * time.Millisecond
	openAIModerationRulesVersion                = "openai-moderation"
)

type ContentModerationVerdictCache interface {
	Get(ctx context.Context, key string) (map[string]float64, bool, error)
	Set(ctx context.Context, key string, scores map[string]float64, ttl time.Duration) error
}

func (s *ContentModerationService) SetVerdictCache(c ContentModerationVerdictCache) {
	if s != nil {
		s.verdictCache = c
	}
}

func contentModerationRulesVersion(cfg *ContentModerationConfig) string {
	if cfg.Engine == ContentModerationEngineTypeSafe {
		return TypeSafeModerationRulesVersion
	}
	return openAIModerationRulesVersion
}

// contentModerationVerdictCacheKey binds a verdict to engine, model and rules so that
// switching any of them never reuses stale scores.
func contentModerationVerdictCacheKey(cfg *ContentModerationConfig, inputHash string) string {
	sum := sha256.Sum256([]byte(cfg.Engine + "\x00" + cfg.Model + "\x00" + contentModerationRulesVersion(cfg) + "\x00" + inputHash))
	return hex.EncodeToString(sum[:])
}

func (s *ContentModerationService) verdictCacheEnabled(cfg *ContentModerationConfig) bool {
	return s != nil && s.verdictCache != nil && cfg != nil && cfg.VerdictCacheMinutes > 0
}

func (s *ContentModerationService) lookupVerdict(cfg *ContentModerationConfig, inputHash string) (*moderationAPIResult, bool) {
	if !s.verdictCacheEnabled(cfg) {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), contentModerationVerdictCacheTimeout)
	defer cancel()
	scores, ok, err := s.verdictCache.Get(ctx, contentModerationVerdictCacheKey(cfg, inputHash))
	if err != nil {
		slog.Warn("content_moderation.verdict_cache_get_failed", "error", err)
		return nil, false
	}
	if !ok || len(scores) == 0 {
		return nil, false
	}
	meta := &ContentModerationEngineMeta{Engine: cfg.Engine, Model: cfg.Model, Cached: true}
	if cfg.Engine == ContentModerationEngineTypeSafe {
		meta.RulesVersion = TypeSafeModerationRulesVersion
	}
	return &moderationAPIResult{CategoryScores: scores, EngineMeta: meta}, true
}

func (s *ContentModerationService) storeVerdict(cfg *ContentModerationConfig, inputHash string, result *moderationAPIResult) {
	if !s.verdictCacheEnabled(cfg) || result == nil || len(result.CategoryScores) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), contentModerationVerdictCacheTimeout)
	defer cancel()
	ttl := time.Duration(cfg.VerdictCacheMinutes) * time.Minute
	if err := s.verdictCache.Set(ctx, contentModerationVerdictCacheKey(cfg, inputHash), result.CategoryScores, ttl); err != nil {
		slog.Warn("content_moderation.verdict_cache_set_failed", "error", err)
	}
}
