package repository

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	pushNotificationSentKeyPrefix = "push_notification:sent:"
	pushNotificationSentTTL       = 8 * 24 * time.Hour
)

type pushNotificationStore struct {
	rdb *redis.Client
}

func NewPushNotificationStore(rdb *redis.Client) service.PushNotificationStore {
	return &pushNotificationStore{rdb: rdb}
}

func (s *pushNotificationStore) IncrSent(ctx context.Context, day, category string) (int64, error) {
	if s == nil || s.rdb == nil {
		return 0, nil
	}
	key := pushNotificationSentKeyPrefix + day
	pipe := s.rdb.TxPipeline()
	incr := pipe.HIncrBy(ctx, key, category, 1)
	pipe.Expire(ctx, key, pushNotificationSentTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

func (s *pushNotificationStore) GetSent(ctx context.Context, day string) (map[string]int64, error) {
	out := map[string]int64{}
	if s == nil || s.rdb == nil {
		return out, nil
	}
	values, err := s.rdb.HGetAll(ctx, pushNotificationSentKeyPrefix+day).Result()
	if err != nil {
		return nil, err
	}
	for k, v := range values {
		if n, parseErr := strconv.ParseInt(v, 10, 64); parseErr == nil {
			out[k] = n
		}
	}
	return out, nil
}

func (s *pushNotificationStore) TryOnce(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if s == nil || s.rdb == nil {
		return true, nil
	}
	return s.rdb.SetNX(ctx, key, "1", ttl).Result()
}
