package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsOpsCriticalSeverity(t *testing.T) {
	require.True(t, isOpsCriticalSeverity("P0"))
	require.True(t, isOpsCriticalSeverity(" p0 "))
	require.False(t, isOpsCriticalSeverity("P1"))
	require.False(t, isOpsCriticalSeverity(""))
}

func TestPushNotifyOpsAlertRespectsSwitch(t *testing.T) {
	value, threshold := 0.42, 0.2
	rule := &OpsAlertRule{ID: 3, Name: "错误率极高", Severity: "P0", MetricType: "error_rate"}
	event := &OpsAlertEvent{Title: "错误率极高", Description: "5 分钟错误率 42%", MetricValue: &value, ThresholdValue: &threshold, FiredAt: time.Now()}

	svc, sender, _ := newEnabledPushService(t, nil)
	svc.NotifyOpsAlert(context.Background(), rule, event)
	drainPushQueue(context.Background(), svc)
	sent := sender.snapshot()
	require.Len(t, sent, 1)
	require.Equal(t, "【P0】错误率极高", sent[0].Title)
	require.Contains(t, sent[0].Desp, "0.42")
	require.Contains(t, sent[0].Desp, "error_rate")

	off, offSender, _ := newEnabledPushService(t, func(c *PushNotificationConfig) { c.OpsCriticalEnabled = false })
	off.NotifyOpsAlert(context.Background(), rule, event)
	drainPushQueue(context.Background(), off)
	require.Empty(t, offSender.snapshot())
}
