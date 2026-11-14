package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func hitAt(userID int64, at time.Time, action, category string) ModerationHitEvent {
	return ModerationHitEvent{UserID: userID, UserEmail: "u@example.com", Category: category, Score: 0.9, Action: action, At: at}
}

func TestModerationHitAggregatorFirstHitImmediateRestMerged(t *testing.T) {
	var a moderationHitAggregator
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	window := 30 * time.Minute

	immediate, expired := a.record(hitAt(1, t0, ContentModerationActionBlock, "sexual"), window)
	require.True(t, immediate)
	require.Nil(t, expired)
	immediate, _ = a.record(hitAt(1, t0.Add(time.Minute), ContentModerationActionBlock, "sexual"), window)
	require.False(t, immediate)
	immediate, _ = a.record(hitAt(1, t0.Add(2*time.Minute), ContentModerationActionHashBlock, "hash"), window)
	require.False(t, immediate)
	immediate, _ = a.record(hitAt(1, t0.Add(3*time.Minute), ContentModerationActionBlock, "violence"), window)
	require.False(t, immediate)

	require.Empty(t, a.flush(t0.Add(29*time.Minute), window))
	sums := a.flush(t0.Add(30*time.Minute), window)
	require.Len(t, sums, 1)
	require.Equal(t, 3, sums[0].Count)
	require.Equal(t, "hash", sums[0].TopCategory, "ties resolve alphabetically")
	require.Empty(t, a.flush(t0.Add(31*time.Minute), window), "window removed after flush")
}

func TestModerationHitAggregatorHashBlockNeverImmediate(t *testing.T) {
	var a moderationHitAggregator
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	window := 30 * time.Minute

	immediate, _ := a.record(hitAt(2, t0, ContentModerationActionHashBlock, "hash"), window)
	require.False(t, immediate)
	immediate, _ = a.record(hitAt(2, t0.Add(time.Minute), ContentModerationActionBlock, "sexual"), window)
	require.True(t, immediate, "first real hit in the window is still pushed")
}

func TestModerationHitAggregatorReturnsExpiredWindowOnNewHit(t *testing.T) {
	var a moderationHitAggregator
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	window := 30 * time.Minute
	a.record(hitAt(3, t0, ContentModerationActionBlock, "sexual"), window)
	a.record(hitAt(3, t0.Add(time.Minute), ContentModerationActionBlock, "sexual"), window)

	immediate, expired := a.record(hitAt(3, t0.Add(40*time.Minute), ContentModerationActionBlock, "sexual"), window)
	require.True(t, immediate)
	require.NotNil(t, expired)
	require.Equal(t, 1, expired.Count)
}

func TestPushNotifyModerationHitPushesFirstAndSummary(t *testing.T) {
	svc, sender, _ := newEnabledPushService(t, nil)
	t0 := time.Now()
	svc.now = func() time.Time { return t0 }
	ctx := context.Background()
	ev := ModerationHitEvent{UserID: 9, UserEmail: "nine@example.com", GroupName: "定制0.8", Model: "claude-opus-5-5", Category: "sexual", Score: 0.93, Action: ContentModerationActionBlock, InputHash: "abc123", RequestID: "req-1", At: t0}

	svc.NotifyModerationHit(ctx, ev)
	ev.At, ev.RequestID = t0.Add(time.Minute), "req-2"
	svc.NotifyModerationHit(ctx, ev)
	drainPushQueue(ctx, svc)
	sent := sender.snapshot()
	require.Len(t, sent, 1)
	require.Contains(t, sent[0].Title, "【拦截】")
	require.Contains(t, sent[0].Desp, "nine@example.com")
	require.Contains(t, sent[0].Desp, "abc123")
	require.Contains(t, sent[0].Desp, "https://api.example.com/admin/risk-control?search=req-1")

	svc.now = func() time.Time { return t0.Add(31 * time.Minute) }
	svc.flushHitWindows(ctx)
	drainPushQueue(ctx, svc)
	sent = sender.snapshot()
	require.Len(t, sent, 2)
	require.Contains(t, sent[1].Title, "又命中1次")
}

func TestPushNotifyModerationHitSkipsWhenDisabled(t *testing.T) {
	svc, sender, _ := newEnabledPushService(t, func(c *PushNotificationConfig) { c.Enabled = false })
	svc.NotifyModerationHit(context.Background(), ModerationHitEvent{UserID: 1, Action: ContentModerationActionBlock, At: time.Now()})
	drainPushQueue(context.Background(), svc)
	require.Empty(t, sender.snapshot())
	require.Empty(t, svc.queue)
}

func TestFormatModerationHitObserveLabel(t *testing.T) {
	msg := formatModerationHit(ModerationHitEvent{UserEmail: "o@example.com", Category: "sexual", Score: 0.9, Action: ContentModerationActionAllow, Observe: true}, "")
	require.Contains(t, msg.title, "【观察】")
	require.Contains(t, msg.desp, "若开拦会被拦")
	require.NotContains(t, msg.desp, "后台查看")
}
