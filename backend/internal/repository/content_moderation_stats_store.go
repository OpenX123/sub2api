package repository

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	contentModerationStatsKeyPrefix = "content_moderation:stats:"
	contentModerationStatsTTL       = 8 * 24 * time.Hour
)

type contentModerationStatsStore struct {
	rdb *redis.Client
}

func NewContentModerationStatsStore(rdb *redis.Client) service.ContentModerationStatsStore {
	return &contentModerationStatsStore{rdb: rdb}
}

func contentModerationStatsFields(stats *service.ContentModerationDailyStats) map[string]*int64 {
	return map[string]*int64{
		"audits": &stats.Audits, "upstream_calls": &stats.UpstreamCalls, "cache_hits": &stats.CacheHits,
		"input_tokens": &stats.InputTokens, "errors": &stats.Errors, "flagged": &stats.Flagged,
		"blocked": &stats.Blocked, "observe_flagged": &stats.ObserveFlagged,
		"hash_blocks": &stats.HashBlocks, "keyword_blocks": &stats.KeywordBlocks,
	}
}

func (s *contentModerationStatsStore) Add(ctx context.Context, day string, delta service.ContentModerationDailyStats) error {
	if s == nil || s.rdb == nil {
		return nil
	}
	key := contentModerationStatsKeyPrefix + day
	pipe := s.rdb.TxPipeline()
	for field, value := range contentModerationStatsFields(&delta) {
		if *value != 0 {
			pipe.HIncrBy(ctx, key, field, *value)
		}
	}
	pipe.Expire(ctx, key, contentModerationStatsTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *contentModerationStatsStore) Get(ctx context.Context, day string) (service.ContentModerationDailyStats, error) {
	var out service.ContentModerationDailyStats
	if s == nil || s.rdb == nil {
		return out, nil
	}
	values, err := s.rdb.HGetAll(ctx, contentModerationStatsKeyPrefix+day).Result()
	if err != nil {
		return out, err
	}
	for field, target := range contentModerationStatsFields(&out) {
		if raw, ok := values[field]; ok {
			if n, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil {
				*target = n
			}
		}
	}
	return out, nil
}
