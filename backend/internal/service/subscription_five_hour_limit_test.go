package service

import (
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// 5h 窗口与 Claude 官方语义一致：首次计费开启窗口，满 5h 过期，
// 过期后的下一次计费开启新窗口（不按原起点的整数倍对齐）。

func newFiveHourTestService(now time.Time) *SubscriptionService {
	svc := NewSubscriptionService(groupRepoNoop{}, userSubRepoNoop{}, nil, nil, nil)
	svc.now = func() time.Time { return now }
	return svc
}

func activeFiveHourSubscription(now time.Time, windowStart *time.Time, usage float64) *UserSubscription {
	day := now.Add(-time.Hour)
	return &UserSubscription{
		Status:             SubscriptionStatusActive,
		StartsAt:           now.Add(-48 * time.Hour),
		ExpiresAt:          now.Add(30 * 24 * time.Hour),
		DailyWindowStart:   &day,
		WeeklyWindowStart:  &day,
		MonthlyWindowStart: &day,
		Window5hStart:      windowStart,
		Usage5hUSD:         usage,
	}
}

func TestValidateAndCheckLimitsRejectsWhenFiveHourWindowIsUsedUp(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-time.Hour)
	sub := activeFiveHourSubscription(now, &windowStart, 10)

	_, err := newFiveHourTestService(now).ValidateAndCheckLimits(sub, &Group{RateLimit5h: ptrFloat64(10)})

	require.ErrorIs(t, err, ErrFiveHourLimitExceeded)
}

func TestValidateAndCheckLimitsAllowsRequestAfterFiveHourWindowExpired(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-5 * time.Hour)
	sub := activeFiveHourSubscription(now, &windowStart, 10)

	_, err := newFiveHourTestService(now).ValidateAndCheckLimits(sub, &Group{RateLimit5h: ptrFloat64(10)})

	require.NoError(t, err, "窗口满 5h 即过期，下一次请求开启新窗口，不能沿用旧用量拒绝")
}

func TestValidateAndCheckLimitsZeroFiveHourLimitBlocksUsage(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	sub := activeFiveHourSubscription(now, nil, 0)

	_, err := newFiveHourTestService(now).ValidateAndCheckLimits(sub, &Group{RateLimit5h: ptrFloat64(0)})

	require.ErrorIs(t, err, ErrFiveHourLimitExceeded, "5h 限额为 0 表示禁止使用")
}

func TestValidateAndCheckLimitsFiveHourErrorCarriesWindowResetTime(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-2 * time.Hour)
	sub := activeFiveHourSubscription(now, &windowStart, 10)

	_, err := newFiveHourTestService(now).ValidateAndCheckLimits(sub, &Group{RateLimit5h: ptrFloat64(10)})

	appErr := infraerrors.FromError(err)
	require.NotNil(t, appErr)
	require.Equal(t, "2026-09-29T15:00:00Z", appErr.Metadata["window_resets_at"])
}
