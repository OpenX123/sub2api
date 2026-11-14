package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeModerationNotifier struct {
	mu     sync.Mutex
	hits   []ModerationHitEvent
	alerts []ModerationSystemAlert
}

func (f *fakeModerationNotifier) NotifyModerationHit(_ context.Context, ev ModerationHitEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits = append(f.hits, ev)
}

func (f *fakeModerationNotifier) NotifySystemAlert(_ context.Context, alert ModerationSystemAlert) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, alert)
}

func (f *fakeModerationNotifier) snapshotAlerts() []ModerationSystemAlert {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ModerationSystemAlert(nil), f.alerts...)
}

func (f *fakeModerationNotifier) snapshotHits() []ModerationHitEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ModerationHitEvent(nil), f.hits...)
}

func TestModerationHealthBalanceAlertRepeatsEvery30MinutesAndResolves(t *testing.T) {
	var h contentModerationHealth
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

	alerts := h.observe(t0, moderationAttemptOutcome{HTTPStatus: http.StatusPaymentRequired})
	require.Len(t, alerts, 1)
	require.Equal(t, ModerationAlertBalanceExhausted, alerts[0].Kind)
	require.False(t, alerts[0].Resolved)

	require.Empty(t, h.observe(t0.Add(10*time.Minute), moderationAttemptOutcome{HTTPStatus: http.StatusPaymentRequired}))
	require.Empty(t, h.observe(t0.Add(11*time.Minute), moderationAttemptOutcome{NoKeyAvailable: true, Err: "frozen"}), "frozen alert is suppressed while balance alert fires")

	again := h.observe(t0.Add(31*time.Minute), moderationAttemptOutcome{HTTPStatus: http.StatusPaymentRequired})
	require.Len(t, again, 1)

	resolved := h.observe(t0.Add(32*time.Minute), moderationAttemptOutcome{Success: true})
	require.Equal(t, []ModerationSystemAlert{{Kind: ModerationAlertBalanceExhausted, Resolved: true, At: t0.Add(32 * time.Minute)}}, resolved)
}

func TestModerationHealthFailureRateNeedsMinimumCalls(t *testing.T) {
	var h contentModerationHealth
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	var fired []ModerationSystemAlert
	for i := 0; i < 19; i++ {
		fired = append(fired, h.observe(t0, moderationAttemptOutcome{HTTPStatus: http.StatusBadGateway})...)
	}
	require.Empty(t, fired, "below 20 calls no rate alert")

	fired = h.observe(t0.Add(time.Minute), moderationAttemptOutcome{HTTPStatus: http.StatusBadGateway})
	require.Len(t, fired, 1)
	require.Equal(t, ModerationAlertFailureRate, fired[0].Kind)
	require.Contains(t, fired[0].Detail, "20/20")

	var resolved []ModerationSystemAlert
	for i := 0; i < 80; i++ {
		resolved = append(resolved, h.observe(t0.Add(2*time.Minute), moderationAttemptOutcome{Success: true})...)
	}
	require.Len(t, resolved, 1)
	require.True(t, resolved[0].Resolved)
}

func TestModerationHealthIgnoresBucketsOlderThanFiveMinutes(t *testing.T) {
	var h contentModerationHealth
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		h.observe(t0, moderationAttemptOutcome{HTTPStatus: http.StatusBadGateway})
	}
	calls, failures := h.windowTotals(t0.Add(6*time.Minute).Unix() / 60)
	require.Zero(t, calls)
	require.Zero(t, failures)
}

func TestContentModerationNotifiesBalanceExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer server.Close()
	notifier := &fakeModerationNotifier{}
	svc := &ContentModerationService{httpClient: server.Client(), keyHealth: map[string]*contentModerationKeyHealth{}}
	svc.SetNotifier(notifier)

	_, err := svc.callModeration(context.Background(), newTypeSafeTestConfig(server.URL), "需要审核的文字")

	require.Error(t, err)
	alerts := notifier.snapshotAlerts()
	require.Len(t, alerts, 1)
	require.Equal(t, ModerationAlertBalanceExhausted, alerts[0].Kind)
}
