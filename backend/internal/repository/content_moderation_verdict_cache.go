package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const contentModerationVerdictKeyPrefix = "content_moderation:verdict:"

type contentModerationVerdictCache struct {
	rdb *redis.Client
}

func NewContentModerationVerdictCache(rdb *redis.Client) service.ContentModerationVerdictCache {
	return &contentModerationVerdictCache{rdb: rdb}
}

func (c *contentModerationVerdictCache) Get(ctx context.Context, key string) (map[string]float64, bool, error) {
	if c == nil || c.rdb == nil {
		return nil, false, nil
	}
	raw, err := c.rdb.Get(ctx, contentModerationVerdictKeyPrefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var scores map[string]float64
	if err := json.Unmarshal(raw, &scores); err != nil {
		return nil, false, err
	}
	return scores, true, nil
}

func (c *contentModerationVerdictCache) Set(ctx context.Context, key string, scores map[string]float64, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	raw, err := json.Marshal(scores)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, contentModerationVerdictKeyPrefix+key, raw, ttl).Err()
}
