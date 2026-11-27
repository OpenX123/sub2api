package service

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestIncidentProbeDebouncesAcrossRounds(t *testing.T) {
	var state map[string]incidentState
	sample := incidentSample{key: "proxy:1", label: "proxy", unhealthy: true, known: true, threshold: 3}
	tick := func(minute int64) (int, int) {
		var fire, rec []string
		state, fire, rec = applyIncidentSamples(state, []incidentSample{sample}, minute)
		return len(fire), len(rec)
	}
	for _, m := range []int64{1, 1, 3, 4} {
		f, r := tick(m)
		require.Zero(t, f+r)
	}
	f, r := tick(5)
	require.Equal(t, 1, f)
	require.Zero(t, r)
	f, r = tick(6)
	require.Zero(t, f+r)
	sample.known = false
	f, r = tick(7)
	require.Zero(t, f+r)
	sample.known = true
	sample.unhealthy = false
	f, r = tick(8)
	require.Zero(t, f)
	require.Equal(t, 1, r)
	f, r = tick(9)
	require.Zero(t, f+r)
}

func TestEstimatedFiveHourRemaining(t *testing.T) {
	now := time.Now()
	end := now.Add(time.Hour)
	p := &UsageProgress{Utilization: 25, WindowStats: &WindowStats{Cost: 10, StandardCost: 100}, ResetsAt: &end}
	remaining, ok := estimatedFiveHourRemaining(p, now)
	require.True(t, ok)
	require.Equal(t, 30.0, remaining)
	p.Utilization = 0
	_, ok = estimatedFiveHourRemaining(p, now)
	require.False(t, ok)
	p.Utilization = 50
	_, ok = estimatedFiveHourRemaining(p, end)
	require.False(t, ok)
}

func TestCredentialOfflineRequiresAuthenticationSignal(t *testing.T) {
	now := time.Now()
	until := now.Add(time.Minute)
	a := &Account{Status: StatusError, ErrorMessage: "Credit balance exhausted (400)"}
	require.False(t, claudeCredentialOffline(a, now))
	a.ErrorMessage = "Token refresh failed (non-retryable): invalid_grant"
	require.True(t, claudeCredentialOffline(a, now))
	a.Status = StatusActive
	a.TempUnschedulableReason = "OAuth 401: token expired"
	a.TempUnschedulableUntil = &until
	require.True(t, claudeCredentialOffline(a, now))
	require.False(t, claudeCredentialOffline(a, until.Add(time.Second)))
}
