package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestPushNotificationStoreCountsAndOnce(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	store := NewPushNotificationStore(client)
	ctx := context.Background()

	n, err := store.IncrSent(ctx, "20261115", "hit")
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	n, _ = store.IncrSent(ctx, "20261115", "hit")
	require.Equal(t, int64(2), n)
	_, _ = store.IncrSent(ctx, "20261115", "alert")

	sent, err := store.GetSent(ctx, "20261115")
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"hit": 2, "alert": 1}, sent)

	ok, err := store.TryOnce(ctx, "push_notification:daily_report:20261115", time.Hour)
	require.NoError(t, err)
	require.True(t, ok)
	ok, _ = store.TryOnce(ctx, "push_notification:daily_report:20261115", time.Hour)
	require.False(t, ok)
}
