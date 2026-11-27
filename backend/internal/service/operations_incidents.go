package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sync/errgroup"
	"log/slog"
	"sort"
	"strings"
	"time"
)

const operationsIncidentStateKey = "operations_incidents_state_v1"

type incidentState struct {
	Active    bool  `json:"active,omitempty"`
	Failures  int   `json:"failures,omitempty"`
	LastProbe int64 `json:"last_probe,omitempty"`
}
type incidentSample struct {
	key, label       string
	unhealthy, known bool
	threshold        int
}

// applyIncidentSamples retains uncertain observations without emitting a recovery.
// Entries that are no longer monitored are retired without reporting them as healthy.
func applyIncidentSamples(previous map[string]incidentState, samples []incidentSample, minute int64) (map[string]incidentState, []string, []string) {
	next := map[string]incidentState{}
	var firing, recovered []string
	for _, sample := range samples {
		st := previous[sample.key]
		if !sample.known {
			st.Failures = 0
			st.LastProbe = 0
		} else if sample.unhealthy {
			if sample.threshold > 1 && !st.Active {
				if st.LastProbe != minute {
					if st.LastProbe != minute-1 {
						st.Failures = 0
					}
					st.Failures++
					st.LastProbe = minute
				}
			}
			if !st.Active && (sample.threshold <= 1 || st.Failures >= sample.threshold) {
				st.Active = true
				st.LastProbe = 0
				st.Failures = 0
				firing = append(firing, sample.label)
			}
		} else {
			if st.Active {
				recovered = append(recovered, sample.label)
			}
			st = incidentState{}
		}
		if st != (incidentState{}) {
			next[sample.key] = st
		}
	}
	sort.Strings(firing)
	sort.Strings(recovered)
	return next, firing, recovered
}

func claudeCredentialOffline(a *Account, now time.Time) bool {
	reason := ""
	switch {
	case a.Status == StatusError:
		reason = a.ErrorMessage
	case a.TempUnschedulableUntil != nil && a.TempUnschedulableUntil.After(now):
		reason = a.TempUnschedulableReason
	}
	reason = strings.ToLower(reason)
	for _, marker := range []string{"authentication failed (401)", "oauth 401", "invalid_grant", "invalid_client", "token refresh failed (non-retryable)", "organization disabled", "token_revoked", "token_invalidated", "identity verification required", "access forbidden (403)"} {
		if strings.Contains(reason, marker) {
			return true
		}
	}
	return false
}

func (s *ContentModerationDailyReportService) monitorIncidents() {
	defer s.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 52*time.Second)
			if err := s.checkIncidents(ctx); err != nil {
				slog.Warn("operations_incidents.check_failed", "error", err)
			}
			cancel()
		}
	}
}

func (s *ContentModerationDailyReportService) checkIncidents(ctx context.Context) error {
	cfg, err := s.push.readConfig(ctx)
	if err != nil {
		return err
	}
	if !cfg.Enabled || !cfg.DigestEnabled || cfg.SendKey == "" {
		return nil
	}
	now := s.now()
	// Keep different replicas from overlapping, even on opposite sides of a minute boundary.
	locked, err := s.pushStore.TryOnce(ctx, "push_notification:incidents:lease", 55*time.Second)
	if err != nil || !locked {
		return err
	}
	prev := map[string]incidentState{}
	raw, err := s.push.settingRepo.GetValue(ctx, operationsIncidentStateKey)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return err
	}
	if raw != "" {
		if err = json.Unmarshal([]byte(raw), &prev); err != nil {
			return err
		}
	}
	accounts, err := s.ops.accountRepo.ListAllWithFilters(ctx, PlatformAnthropic, "", "", "", 0, "")
	if err != nil {
		return err
	}
	samples := make([]incidentSample, 0, len(accounts)+1)
	for i := range accounts {
		a := &accounts[i]
		samples = append(samples, incidentSample{key: fmt.Sprintf("account:%d", a.ID), label: fmt.Sprintf("Claude 账号 #%d · 凭据状态", a.ID), unhealthy: claudeCredentialOffline(a, now), known: true, threshold: 1})
	}
	if edu := s.tierCapacity(ctx, accounts, nil, true, now)["edu"]; edu != nil {
		// Unknown balances are part of the denominator. An ambiguous majority cannot resolve an existing incident.
		unknown := edu.total - edu.covered
		low := edu.low*2 >= edu.total
		known := low || (edu.low+unknown)*2 < edu.total
		samples = append(samples, incidentSample{key: "edu", label: fmt.Sprintf("EDU 5H 剩余额度低于 $50：%d/%d 个，未知 %d 个", edu.low, edu.total, unknown), unhealthy: low, known: known, threshold: 1})
	}
	proxies, err := s.admin.GetAllProxiesWithAccountCount(ctx)
	if err != nil {
		slog.Warn("operations_incidents.proxy_list_failed", "error", err)
		for key := range prev {
			if strings.HasPrefix(key, "proxy:") {
				samples = append(samples, incidentSample{key: key})
			}
		}
		proxies = nil
	}
	probeSamples := make([]incidentSample, len(proxies))
	probeRound, stopProbes := context.WithTimeout(ctx, 30*time.Second)
	defer stopProbes()
	var probes errgroup.Group
	probes.SetLimit(12)
	for i, p := range proxies {
		if p.AccountCount == 0 {
			continue
		}
		probes.Go(func() error {
			sample := incidentSample{key: fmt.Sprintf("proxy:%d", p.ID), label: fmt.Sprintf("代理 #%d · %s:%d（关联 %d 个账号）", p.ID, p.Host, p.Port, p.AccountCount), threshold: 3}
			if probeRound.Err() == nil {
				probeCtx, cancel := context.WithTimeout(probeRound, 8*time.Second)
				result, err := s.admin.TestProxy(probeCtx, p.ID)
				cancel()
				// A local repository error or outer round cancellation is an unknown observation.
				if err == nil && result != nil && probeRound.Err() == nil {
					sample.known = true
					sample.unhealthy = !result.Success
				}
			}
			probeSamples[i] = sample
			return nil
		})
	}
	_ = probes.Wait()
	for _, p := range probeSamples {
		if p.key != "" {
			samples = append(samples, p)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	next, firing, recovered := applyIncidentSamples(prev, samples, now.Unix()/60)
	state, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if len(firing)+len(recovered) > 0 {
		sender, ok := s.push.sender.(interface {
			SendIncidentCard(context.Context, string, []string, []string) error
		})
		if !ok {
			return errors.New("incident card sender unavailable")
		}
		if err = sender.SendIncidentCard(ctx, cfg.SendKey, firing, recovered); err != nil {
			return err
		}
	}
	if string(state) == raw {
		return nil
	}
	return s.push.settingRepo.Set(ctx, operationsIncidentStateKey, string(state))
}
