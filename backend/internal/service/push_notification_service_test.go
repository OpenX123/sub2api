package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newEnabledPushService(t *testing.T, mutate func(*PushNotificationConfig)) (*PushNotificationService, *fakePushSender, *fakePushStore) {
	t.Helper()
	cfg := defaultPushNotificationConfig()
	cfg.Enabled, cfg.SendKey = true, "SCT470902TabcDEF123"
	if mutate != nil {
		mutate(cfg)
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	sender, store := &fakePushSender{}, &fakePushStore{}
	svc := NewPushNotificationService(&contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyPushNotificationConfig: string(raw),
		SettingKeyFrontendURL:            "https://api.example.com/",
	}}, store, sender)
	svc.retryDelay = 0
	return svc, sender, store
}

// drainPushQueue delivers everything queued without running the background loop.
func drainPushQueue(ctx context.Context, svc *PushNotificationService) {
	for {
		select {
		case msg := <-svc.queue:
			svc.deliver(ctx, msg)
		default:
			return
		}
	}
}

func TestPushDeliverRespectsHitDailyCapAndSendsNoticeOnce(t *testing.T) {
	svc, sender, store := newEnabledPushService(t, func(c *PushNotificationConfig) { c.HitDailyCap = 2 })
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		svc.deliver(ctx, pushMessage{category: PushCategoryHit, title: "hit", desp: "d"})
	}
	sent := sender.snapshot()
	require.Len(t, sent, 3)
	require.Equal(t, "hit", sent[0].Title)
	require.Equal(t, "hit", sent[1].Title)
	require.Contains(t, sent[2].Title, "上限")
	counts, _ := store.GetSent(ctx, pushDay(time.Now()))
	require.Equal(t, int64(5), counts["hit"])
	require.Equal(t, int64(1), counts["notice"])
}

func TestPushDeliverRetriesOnce(t *testing.T) {
	svc, sender, _ := newEnabledPushService(t, nil)
	sender.failTimes = 1
	svc.deliver(context.Background(), pushMessage{category: PushCategoryAlert, title: "a", desp: "d"})
	require.Len(t, sender.snapshot(), 1)

	sender.failTimes = 2
	svc.deliver(context.Background(), pushMessage{category: PushCategoryAlert, title: "b", desp: "d"})
	require.Len(t, sender.snapshot(), 1, "gives up after one retry")
}

func TestPushDeliverSkipsWhenDisabled(t *testing.T) {
	svc, sender, _ := newEnabledPushService(t, func(c *PushNotificationConfig) { c.Enabled = false })
	svc.deliver(context.Background(), pushMessage{category: PushCategoryAlert, title: "a", desp: "d"})
	require.Empty(t, sender.snapshot())
}

func TestPushSystemAlertFormatting(t *testing.T) {
	firing := formatSystemAlert(ModerationSystemAlert{Kind: ModerationAlertBalanceExhausted, Detail: "TypeSafe 返回 402", At: time.Now()})
	require.Equal(t, PushCategoryAlert, firing.category)
	require.Contains(t, firing.title, "【告警】")
	require.Contains(t, firing.title, "402")
	require.Contains(t, firing.desp, "放行")

	resolved := formatSystemAlert(ModerationSystemAlert{Kind: ModerationAlertBalanceExhausted, Resolved: true, At: time.Now()})
	require.Contains(t, resolved.title, "【恢复】")
}

func TestPushServiceQueueDeliversAsync(t *testing.T) {
	svc, sender, _ := newEnabledPushService(t, nil)
	svc.Start()
	defer svc.Stop()
	svc.NotifySystemAlert(context.Background(), ModerationSystemAlert{Kind: ModerationAlertFailureRate, Detail: "x", At: time.Now()})
	require.Eventually(t, func() bool { return len(sender.snapshot()) == 1 }, 2*time.Second, 10*time.Millisecond)
}
