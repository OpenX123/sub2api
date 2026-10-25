package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// subscriptionCacheHitStub 让订阅缓存命中给定数据，模拟 Redis 中的实时用量。
type subscriptionCacheHitStub struct {
	billingCacheWorkerStub
	data *SubscriptionCacheData
}

func (s *subscriptionCacheHitStub) GetSubscriptionCache(ctx context.Context, userID, groupID int64) (*SubscriptionCacheData, error) {
	return s.data, nil
}

func checkSubscriptionWithCache(t *testing.T, data *SubscriptionCacheData, group *Group, sub *UserSubscription) error {
	t.Helper()
	svc := NewBillingCacheService(&subscriptionCacheHitStub{data: data}, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	group.ID = 7
	group.SubscriptionType = SubscriptionTypeSubscription
	return svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, group, sub, PlatformAnthropic)
}

func TestCheckBillingEligibilityRejectsWhenCachedFiveHourWindowIsUsedUp(t *testing.T) {
	windowStart := time.Now().Add(-time.Hour).Truncate(time.Second)
	data := &SubscriptionCacheData{
		Status:        SubscriptionStatusActive,
		ExpiresAt:     time.Now().Add(24 * time.Hour),
		Usage5h:       10,
		Window5hStart: &windowStart,
	}

	err := checkSubscriptionWithCache(t, data, &Group{RateLimit5h: ptrFloat64(10)}, &UserSubscription{})

	require.ErrorIs(t, err, ErrFiveHourLimitExceeded)
	resetAt, parseErr := time.Parse(time.RFC3339, infraerrors.FromError(err).Metadata["window_resets_at"])
	require.NoError(t, parseErr)
	require.True(t, resetAt.Equal(windowStart.Add(5*time.Hour)))
}

func TestCheckBillingEligibilityAllowsWhenCachedFiveHourWindowExpired(t *testing.T) {
	windowStart := time.Now().Add(-5*time.Hour - time.Minute)
	data := &SubscriptionCacheData{
		Status:        SubscriptionStatusActive,
		ExpiresAt:     time.Now().Add(24 * time.Hour),
		Usage5h:       10,
		Window5hStart: &windowStart,
	}

	err := checkSubscriptionWithCache(t, data, &Group{RateLimit5h: ptrFloat64(10)}, &UserSubscription{})

	require.NoError(t, err, "缓存里的 5h 窗口已过期，下一次请求开启新窗口")
}

func TestCheckBillingEligibilityWeeklyLimitErrorCarriesWindowResetTime(t *testing.T) {
	weekStart := time.Now().Add(-2 * 24 * time.Hour).Truncate(time.Second)
	data := &SubscriptionCacheData{
		Status:      SubscriptionStatusActive,
		ExpiresAt:   time.Now().Add(30 * 24 * time.Hour),
		WeeklyUsage: 50,
	}
	sub := &UserSubscription{
		StartsAt:          weekStart,
		ExpiresAt:         data.ExpiresAt,
		WeeklyWindowStart: &weekStart,
	}

	err := checkSubscriptionWithCache(t, data, &Group{WeeklyLimitUSD: ptrFloat64(50)}, sub)

	require.ErrorIs(t, err, ErrWeeklyLimitExceeded)
	require.NotEmpty(t, infraerrors.FromError(err).Metadata["window_resets_at"])
}
