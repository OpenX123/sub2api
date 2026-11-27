package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"
)

type tierCapacity struct {
	total, ready, inUse, capacity, covered, low int
	remaining                                   float64
}

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

func (s *ContentModerationDailyReportService) tierCapacity(ctx context.Context, accounts []Account, load map[int64]int, eduOnly bool, now time.Time) map[string]*tierCapacity {
	result := map[string]*tierCapacity{}
	var ids []int64
	var starts []time.Time
	windows := make(map[int64]*UsageInfo)
	tiers := make(map[int64]*tierCapacity)
	for i := range accounts {
		a := &accounts[i]
		tier := ClaudeSubscriptionTier(a.Name, a.Extra)
		if eduOnly && tier != "edu" {
			continue
		}
		bucket := result[tier]
		if bucket == nil {
			bucket = &tierCapacity{}
			result[tier] = bucket
		}
		bucket.total++
		bucket.inUse += load[a.ID]
		if a.IsSchedulable() {
			bucket.ready++
			bucket.capacity += a.Concurrency
		}
		if !supportsAnthropicPassiveUsage(a) || s.usage == nil {
			continue
		}
		info := s.usage.estimateSetupTokenUsage(a)
		w := info.FiveHour
		if w == nil || w.Utilization <= 0 || w.ResetsAt == nil || !w.ResetsAt.After(now) || w.ResetsAt.Add(-5*time.Hour).After(now) {
			continue
		}
		windows[a.ID] = info
		tiers[a.ID] = bucket
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
			costs, err := reader.FiveHourWindowCosts(ctx, ids, starts)
			if err != nil {
				slog.Warn("operations_capacity.cost_failed", "error", err)
			} else {
				for id, cost := range costs {
					windows[id].FiveHour.WindowStats = &WindowStats{Cost: cost}
				}
			}
		}
	}
	for id, info := range windows {
		bucket := tiers[id]
		if rem, ok := estimatedFiveHourRemaining(info.FiveHour, now); ok {
			bucket.covered++
			bucket.remaining += rem
			if rem < 50 {
				bucket.low++
			}
		}
	}
	return result
}

func tierCapacityRows(tiers map[string]*tierCapacity) []string {
	names := []struct{ key, label string }{{"pro", "Pro"}, {"max", "Max"}, {"ultra", "Ultra"}, {"edu", "EDU"}, {"unknown", "未分类"}}
	rows := make([]string, 0, len(names))
	for _, name := range names {
		b := tiers[name.key]
		if b == nil {
			b = &tierCapacity{}
		}
		balance := "未知"
		if b.covered > 0 {
			balance = fmt.Sprintf("$%.2f", b.remaining)
		}
		rows = append(rows, fmt.Sprintf("%s · 可用 %d/%d · 并发 %d/%d · 5H 估算剩余 %s（覆盖 %d/%d）", name.label, b.ready, b.total, b.inUse, b.capacity, balance, b.covered, b.total))
	}
	return rows
}
