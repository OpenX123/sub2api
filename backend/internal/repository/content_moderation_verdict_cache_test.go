package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestContentModerationVerdictCacheRoundTrip(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	cache := NewContentModerationVerdictCache(client)
	ctx := context.Background()

	_, ok, err := cache.Get(ctx, "k1")
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, cache.Set(ctx, "k1", map[string]float64{"sexual": 0.12}, time.Hour))
	scores, ok, err := cache.Get(ctx, "k1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 0.12, scores["sexual"])
	require.Equal(t, time.Hour, server.TTL("content_moderation:verdict:k1"))
}
