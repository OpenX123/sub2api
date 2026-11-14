package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestContentModerationStatsStoreAccumulatesPerDay(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	store := NewContentModerationStatsStore(client)
	ctx := context.Background()

	require.NoError(t, store.Add(ctx, "20261115", service.ContentModerationDailyStats{Audits: 1, UpstreamCalls: 1, InputTokens: 900}))
	require.NoError(t, store.Add(ctx, "20261115", service.ContentModerationDailyStats{Audits: 1, CacheHits: 1, Flagged: 1, Blocked: 1}))
	require.NoError(t, store.Add(ctx, "20261116", service.ContentModerationDailyStats{Audits: 5}))

	got, err := store.Get(ctx, "20261115")
	require.NoError(t, err)
	require.Equal(t, service.ContentModerationDailyStats{Audits: 2, UpstreamCalls: 1, CacheHits: 1, InputTokens: 900, Flagged: 1, Blocked: 1}, got)
	require.Greater(t, server.TTL("content_moderation:stats:20261115").Hours(), 24.0*7)

	empty, err := store.Get(ctx, "20200101")
	require.NoError(t, err)
	require.Zero(t, empty)
}
