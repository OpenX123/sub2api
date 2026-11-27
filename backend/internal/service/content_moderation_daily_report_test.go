package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeModerationReportRepo struct {
	top      []ContentModerationUserHitCount
	from, to time.Time
}

func (f *fakeModerationReportRepo) OperationsDigest(context.Context, time.Time, time.Time, time.Time) ([]string, error) {
	return nil, nil
}

func (f *fakeModerationReportRepo) TopFlaggedUsers(_ context.Context, from, to time.Time, _ int) ([]ContentModerationUserHitCount, error) {
	f.from, f.to = from, to
	return f.top, nil
}

func TestContentModerationDailyReportSendsOncePerDayAfterHour(t *testing.T) {
	push, sender, pushStore := newEnabledPushService(t, nil)
	stats := &fakeModerationStatsStore{total: ContentModerationDailyStats{Audits: 1000, UpstreamCalls: 400, CacheHits: 600, InputTokens: 2_000_000, Flagged: 12, Blocked: 9, ObserveFlagged: 3, HashBlocks: 4}}
	reports := &fakeModerationReportRepo{top: []ContentModerationUserHitCount{{UserID: 16, UserEmail: "big@example.com", Hits: 8, Blocks: 0}}}
	svc := NewContentModerationDailyReportService(push, stats, reports, pushStore)
	loc := ContentModerationReportLocation()
	ctx := context.Background()

	svc.now = func() time.Time { return time.Date(2026, 11, 15, 8, 59, 0, 0, loc) }
	require.False(t, svc.tick(ctx), "before 09:00")

	svc.now = func() time.Time { return time.Date(2026, 11, 15, 9, 1, 0, 0, loc) }
	require.True(t, svc.tick(ctx))
	require.False(t, svc.tick(ctx), "only once per day")
	drainPushQueue(ctx, push)

	sent := sender.snapshot()
	require.Len(t, sent, 1)
	require.Equal(t, "【日报】内容审计 11-14", sent[0].Title)
	require.Contains(t, sent[0].Desp, "1000")
	require.Contains(t, sent[0].Desp, "60.0%")
	require.Contains(t, sent[0].Desp, "$0.08")
	require.Contains(t, sent[0].Desp, "big@example.com")
	require.Equal(t, time.Date(2026, 11, 14, 0, 0, 0, 0, loc), reports.from)
	require.Equal(t, time.Date(2026, 11, 15, 0, 0, 0, 0, loc), reports.to)
}

func TestContentModerationDailyReportSkipsWhenDisabled(t *testing.T) {
	push, sender, pushStore := newEnabledPushService(t, func(c *PushNotificationConfig) { c.DailyReportEnabled = false })
	svc := NewContentModerationDailyReportService(push, &fakeModerationStatsStore{}, &fakeModerationReportRepo{}, pushStore)
	svc.now = func() time.Time { return time.Date(2026, 11, 15, 10, 0, 0, 0, ContentModerationReportLocation()) }
	require.False(t, svc.tick(context.Background()))
	require.Empty(t, sender.snapshot())
}

func TestBuildContentModerationDailyReportWithoutHits(t *testing.T) {
	msg := buildContentModerationDailyReport(time.Date(2026, 11, 14, 0, 0, 0, 0, ContentModerationReportLocation()), ContentModerationDailyStats{}, nil, nil)
	require.Equal(t, PushCategoryReport, msg.category)
	require.Contains(t, msg.desp, "无")
}
