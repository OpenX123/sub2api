package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakePush struct{ Key, Title, Desp string }

type fakePushSender struct {
	mu        sync.Mutex
	sent      []fakePush
	failTimes int
}

func (f *fakePushSender) Send(_ context.Context, key, title, desp string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failTimes > 0 {
		f.failTimes--
		return errors.New("send failed")
	}
	f.sent = append(f.sent, fakePush{Key: key, Title: title, Desp: desp})
	return nil
}

func (f *fakePushSender) snapshot() []fakePush {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakePush(nil), f.sent...)
}

type fakePushStore struct {
	mu   sync.Mutex
	sent map[string]map[string]int64
	once map[string]bool
}

func (f *fakePushStore) IncrSent(_ context.Context, day, category string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sent == nil {
		f.sent = map[string]map[string]int64{}
	}
	if f.sent[day] == nil {
		f.sent[day] = map[string]int64{}
	}
	f.sent[day][category]++
	return f.sent[day][category], nil
}

func (f *fakePushStore) GetSent(_ context.Context, day string) (map[string]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int64{}
	for k, v := range f.sent[day] {
		out[k] = v
	}
	return out, nil
}

func (f *fakePushStore) TryOnce(_ context.Context, key string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.once == nil {
		f.once = map[string]bool{}
	}
	if f.once[key] {
		return false, nil
	}
	f.once[key] = true
	return true, nil
}

func TestPushNotificationConfigMasksKeyAndValidates(t *testing.T) {
	repo := &contentModerationTestSettingRepo{values: map[string]string{}}
	svc := NewPushNotificationService(repo, &fakePushStore{}, &fakePushSender{})
	ctx := context.Background()

	view, err := svc.GetConfig(ctx)
	require.NoError(t, err)
	require.False(t, view.Enabled)
	require.Equal(t, 300, view.HitDailyCap)
	require.Equal(t, 30, view.HitWindowMinutes)
	require.Equal(t, 9, view.DailyReportHour)
	require.True(t, view.DailyReportEnabled)
	require.True(t, view.OpsCriticalEnabled)

	enabled := true
	_, err = svc.UpdateConfig(ctx, UpdatePushNotificationConfigInput{Enabled: &enabled})
	require.ErrorContains(t, err, "SendKey")

	bad := "not-a-key"
	_, err = svc.UpdateConfig(ctx, UpdatePushNotificationConfigInput{SendKey: &bad})
	require.ErrorContains(t, err, "SCT")

	key := "SCT470902TabcDEF123"
	hitCap := 5000
	view, err = svc.UpdateConfig(ctx, UpdatePushNotificationConfigInput{Enabled: &enabled, SendKey: &key, HitDailyCap: &hitCap})
	require.NoError(t, err)
	require.True(t, view.SendKeyConfigured)
	require.Equal(t, "********F123", view.SendKeyMasked)
	require.Equal(t, 1000, view.HitDailyCap)
	raw, _ := json.Marshal(view)
	require.NotContains(t, string(raw), key)

	// omitting send_key keeps the stored key
	hour := 8
	view, err = svc.UpdateConfig(ctx, UpdatePushNotificationConfigInput{DailyReportHour: &hour})
	require.NoError(t, err)
	require.True(t, view.SendKeyConfigured)
	require.Equal(t, 8, view.DailyReportHour)
	require.Equal(t, key, svc.loadConfig(ctx).SendKey)

	// clearing the key while enabled is rejected
	_, err = svc.UpdateConfig(ctx, UpdatePushNotificationConfigInput{ClearSendKey: true})
	require.ErrorContains(t, err, "SendKey")
}

func TestPushNotificationSendTestBypassesQueue(t *testing.T) {
	key := "SCT470902TabcDEF123"
	raw, _ := json.Marshal(PushNotificationConfig{Enabled: false, SendKey: key})
	sender := &fakePushSender{}
	svc := NewPushNotificationService(&contentModerationTestSettingRepo{values: map[string]string{SettingKeyPushNotificationConfig: string(raw)}}, &fakePushStore{}, sender)

	require.NoError(t, svc.SendTest(context.Background()))
	sent := sender.snapshot()
	require.Len(t, sent, 1)
	require.Equal(t, key, sent[0].Key)
	require.Contains(t, sent[0].Title, "测试")
}
