package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakePushAccountSource struct {
	accounts []Account
	err      error
	statuses []string
}

func (f *fakePushAccountSource) ListAllWithFilters(_ context.Context, _, _, status, _ string, _ int64, _ string) ([]Account, error) {
	f.statuses = append(f.statuses, status)
	return f.accounts, f.err
}

func errorAccount(id int64, name, msg string, updated time.Time) Account {
	return Account{
		ID: id, Name: name, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Status: StatusError, ErrorMessage: msg, UpdatedAt: updated,
		Groups: []*Group{{ID: 18, Name: "ccmax"}, {ID: 22, Name: "x1"}},
	}
}

func TestPushAccountErrorsMergedAndDeduplicated(t *testing.T) {
	svc, sender, _ := newEnabledPushService(t, nil)
	at := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	src := &fakePushAccountSource{accounts: []Account{
		errorAccount(161, "a@example.com", "Token refresh failed (non-retryable)", at),
		errorAccount(121, "b@example.com", "Access forbidden (403)", at.Add(time.Minute)),
	}}
	svc.SetAccountSource(src)
	ctx := context.Background()

	svc.checkAccountErrors(ctx)
	drainPushQueue(ctx, svc)
	sent := sender.snapshot()
	require.Len(t, sent, 1, "accounts failing together are merged into one push")
	require.Equal(t, "【账号异常】2 个账号停止调度", sent[0].Title)
	require.Contains(t, sent[0].Desp, "b@example.com")
	require.Contains(t, sent[0].Desp, "分组 ccmax、x1")
	require.Contains(t, sent[0].Desp, "Access forbidden (403)")
	require.Contains(t, sent[0].Desp, "https://api.example.com/admin/accounts")
	require.Equal(t, []string{StatusError}, src.statuses)

	// 同一次异常不会重复推送。
	svc.checkAccountErrors(ctx)
	drainPushQueue(ctx, svc)
	require.Len(t, sender.snapshot(), 1)

	// 账号恢复后再次异常（updated_at 变化）会重新推送，且单账号标题带账号名。
	src.accounts = []Account{errorAccount(161, "a@example.com", "Token refresh failed again", at.Add(time.Hour))}
	svc.checkAccountErrors(ctx)
	drainPushQueue(ctx, svc)
	sent = sender.snapshot()
	require.Len(t, sent, 2)
	require.Equal(t, "【账号异常】a@example.com", sent[1].Title)
}

func TestPushAccountErrorsRespectSwitches(t *testing.T) {
	src := &fakePushAccountSource{accounts: []Account{errorAccount(1, "a", "boom", time.Now())}}

	svc, sender, _ := newEnabledPushService(t, func(c *PushNotificationConfig) { c.AccountAlertEnabled = false })
	svc.SetAccountSource(src)
	svc.checkAccountErrors(context.Background())
	drainPushQueue(context.Background(), svc)
	require.Empty(t, sender.snapshot())
	require.Empty(t, src.statuses, "disabled switch must not query accounts")

	svc, sender, _ = newEnabledPushService(t, func(c *PushNotificationConfig) { c.Enabled = false })
	svc.SetAccountSource(src)
	svc.checkAccountErrors(context.Background())
	drainPushQueue(context.Background(), svc)
	require.Empty(t, sender.snapshot())
}

func TestPushAccountErrorsListFailureAndTruncation(t *testing.T) {
	svc, sender, _ := newEnabledPushService(t, nil)
	src := &fakePushAccountSource{err: fmt.Errorf("db down")}
	svc.SetAccountSource(src)
	svc.checkAccountErrors(context.Background())
	drainPushQueue(context.Background(), svc)
	require.Empty(t, sender.snapshot())

	src.err = nil
	at := time.Now()
	for i := 0; i < pushAccountErrorMaxLines+3; i++ {
		src.accounts = append(src.accounts, errorAccount(int64(100+i), fmt.Sprintf("acc-%d", i), "", at.Add(time.Duration(i)*time.Second)))
	}
	svc.checkAccountErrors(context.Background())
	drainPushQueue(context.Background(), svc)
	sent := sender.snapshot()
	require.Len(t, sent, 1)
	require.Contains(t, sent[0].Desp, "另有 3 个账号")
	require.Contains(t, sent[0].Desp, "未记录错误信息")
}

func TestPushConfigAccountAlertDefaultsOnForOldConfig(t *testing.T) {
	cfg, err := parsePushNotificationConfig(`{"enabled":true,"send_key":"SCT1"}`)
	require.NoError(t, err)
	require.True(t, cfg.AccountAlertEnabled)
	require.True(t, cfg.view().AccountAlertEnabled)
}
