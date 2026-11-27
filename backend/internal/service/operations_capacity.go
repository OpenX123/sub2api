package service

import (
	"context"
	"log/slog"
	"math"
	"regexp"
	"time"
)

var eduAccountName = regexp.MustCompile(`(?i)(^|[^a-z])edu([^a-z]|$)|教育`)

type eduCapacity struct{ total, covered, low int }

func estimatedFiveHourRemaining(window *UsageProgress, now time.Time) (float64, bool) {
	if window == nil || window.UtilizationUnknown || window.WindowStats == nil || window.ResetsAt == nil || !window.ResetsAt.After(now) || window.ResetsAt.Add(-5*time.Hour).After(now) {
		return 0, false
	}
	cost, used := window.WindowStats.Cost, window.Utilization
	if cost <= 0 || used <= 0 {
		return 0, false
	}
	remaining := math.Max(0, cost*100/used-cost)
	return remaining, !math.IsNaN(remaining) && !math.IsInf(remaining, 0)
}

func (s *ContentModerationDailyReportService) eduCapacity(ctx context.Context, accounts []Account, now time.Time) eduCapacity {
	var result eduCapacity
	var ids []int64
	var starts []time.Time
	windows := make(map[int64]*UsageInfo)
	for i := range accounts {
		a := &accounts[i]
		if !eduAccountName.MatchString(a.Name) {
			continue
		}
		result.total++
		if !supportsAnthropicPassiveUsage(a) || s.usage == nil {
			continue
		}
		info := s.usage.estimateSetupTokenUsage(a)
		w := info.FiveHour
		if w == nil || w.Utilization <= 0 || w.ResetsAt == nil || !w.ResetsAt.After(now) || w.ResetsAt.Add(-5*time.Hour).After(now) {
			continue
		}
		windows[a.ID] = info
		if a.IsSyntheticUITest() {
			applySyntheticWindowStats(info, a.Extra)
		}
		if w.WindowStats == nil {
			ids = append(ids, a.ID)
			starts = append(starts, w.ResetsAt.Add(-5*time.Hour))
		}
	}
	if len(ids) > 0 {
		reader, ok := s.usage.usageLogRepo.(interface {
			FiveHourWindowCosts(context.Context, []int64, []time.Time) (map[int64]float64, error)
		})
		if ok {
			costCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			costs, err := reader.FiveHourWindowCosts(costCtx, ids, starts)
			cancel()
			if err != nil {
				slog.Warn("operations_capacity.cost_failed", "error", err)
			} else {
				for id, cost := range costs {
					windows[id].FiveHour.WindowStats = &WindowStats{Cost: cost}
				}
			}
		}
	}
	for _, info := range windows {
		if rem, ok := estimatedFiveHourRemaining(info.FiveHour, now); ok {
			result.covered++
			if rem < 50 {
				result.low++
			}
		}
	}
	return result
}
