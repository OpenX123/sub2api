//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestContentModerationHashCache_SafeResultRoundTripAndExpiry(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := NewContentModerationHashCache(rdb)
	ctx := context.Background()

	miss, err := cache.GetSafeModerationResult(ctx, "k1")
	require.NoError(t, err)
	require.Nil(t, miss)

	want := &service.ContentModerationCachedResult{
		CategoryScores: map[string]float64{"harassment": 0.02, "sexual": 0.01},
		EngineMeta:     &service.ContentModerationEngineMeta{Engine: "typesafe", Model: "jev-1.13.0", RulesVersion: "r1"},
	}
	require.NoError(t, cache.SetSafeModerationResult(ctx, "k1", want, time.Minute))
	require.True(t, mr.Exists(contentModerationSafeResultKeyPrefix+"k1"))
	require.Equal(t, time.Minute, mr.TTL(contentModerationSafeResultKeyPrefix+"k1"))

	got, err := cache.GetSafeModerationResult(ctx, "k1")
	require.NoError(t, err)
	require.Equal(t, want, got)

	mr.FastForward(2 * time.Minute)
	expired, err := cache.GetSafeModerationResult(ctx, "k1")
	require.NoError(t, err)
	require.Nil(t, expired)
}

func TestContentModerationHashCache_SafeResultIgnoresInvalidWrites(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := NewContentModerationHashCache(rdb)
	ctx := context.Background()
	result := &service.ContentModerationCachedResult{CategoryScores: map[string]float64{"hate": 0.1}}

	require.NoError(t, cache.SetSafeModerationResult(ctx, " ", result, time.Minute))
	require.NoError(t, cache.SetSafeModerationResult(ctx, "k", nil, time.Minute))
	require.NoError(t, cache.SetSafeModerationResult(ctx, "k", result, 0))
	require.Empty(t, mr.Keys())
}
