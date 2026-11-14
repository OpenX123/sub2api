package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	contentModerationDailyReportTick     = time.Minute
	contentModerationDailyReportTopUsers = 5
	contentModerationDailyReportOnceTTL  = 48 * time.Hour
	jevUSDPerMillionInputTokens          = 0.042
)

type ContentModerationUserHitCount struct {
	UserID    int64
	UserEmail string
	Hits      int64
	Blocks    int64
}

type ContentModerationReportRepository interface {
	TopFlaggedUsers(ctx context.Context, from, to time.Time, limit int) ([]ContentModerationUserHitCount, error)
}

// ContentModerationDailyReportService pushes yesterday's audit summary once per Beijing day.
type ContentModerationDailyReportService struct {
	push      *PushNotificationService
	stats     ContentModerationStatsStore
	reports   ContentModerationReportRepository
	pushStore PushNotificationStore
	now       func() time.Time
	stop      chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup
}

func NewContentModerationDailyReportService(push *PushNotificationService, stats ContentModerationStatsStore, reports ContentModerationReportRepository, pushStore PushNotificationStore) *ContentModerationDailyReportService {
	return &ContentModerationDailyReportService{push: push, stats: stats, reports: reports, pushStore: pushStore, now: time.Now, stop: make(chan struct{})}
}

func (s *ContentModerationDailyReportService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			ticker := time.NewTicker(contentModerationDailyReportTick)
			defer ticker.Stop()
			for {
				select {
				case <-s.stop:
					return
				case <-ticker.C:
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					s.tick(ctx)
					cancel()
				}
			}
		}()
	})
}

func (s *ContentModerationDailyReportService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stop) })
	s.wg.Wait()
}

// tick sends yesterday's report once per Beijing day after the configured hour.
func (s *ContentModerationDailyReportService) tick(ctx context.Context) bool {
	if s == nil || s.push == nil {
		return false
	}
	cfg := s.push.loadConfig(ctx)
	if !cfg.Enabled || !cfg.DailyReportEnabled {
		return false
	}
	loc := ContentModerationReportLocation()
	now := s.now().In(loc)
	if now.Hour() < cfg.DailyReportHour {
		return false
	}
	if s.pushStore != nil {
		ok, err := s.pushStore.TryOnce(ctx, "push_notification:daily_report:"+now.Format("20060102"), contentModerationDailyReportOnceTTL)
		if err != nil || !ok {
			return false
		}
	}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -1)
	dayEnd := dayStart.AddDate(0, 0, 1)
	day := dayStart.Format("20060102")
	var stats ContentModerationDailyStats
	if s.stats != nil {
		got, err := s.stats.Get(ctx, day)
		if err != nil {
			slog.Warn("content_moderation.daily_report_stats_failed", "error", err)
		}
		stats = got
	}
	var top []ContentModerationUserHitCount
	if s.reports != nil {
		got, err := s.reports.TopFlaggedUsers(ctx, dayStart, dayEnd, contentModerationDailyReportTopUsers)
		if err != nil {
			slog.Warn("content_moderation.daily_report_top_users_failed", "error", err)
		}
		top = got
	}
	var sent map[string]int64
	if s.pushStore != nil {
		sent, _ = s.pushStore.GetSent(ctx, day)
	}
	return s.push.enqueue(buildContentModerationDailyReport(dayStart, stats, top, sent))
}

func buildContentModerationDailyReport(day time.Time, stats ContentModerationDailyStats, top []ContentModerationUserHitCount, sent map[string]int64) pushMessage {
	cacheRate := 0.0
	if stats.Audits > 0 {
		cacheRate = float64(stats.CacheHits) / float64(stats.Audits) * 100
	}
	cost := float64(stats.InputTokens) / 1_000_000 * jevUSDPerMillionInputTokens
	blocked := stats.Blocked + stats.HashBlocks + stats.KeywordBlocks
	var b strings.Builder
	fmt.Fprintf(&b, "### %s 内容审计\n\n| 指标 | 数值 |\n|---|---|\n", day.Format("2006-01-02"))
	fmt.Fprintf(&b, "| 审计次数 | %d |\n| 实际调用引擎 | %d |\n| 缓存命中 | %d（%.1f%%） |\n", stats.Audits, stats.UpstreamCalls, stats.CacheHits, cacheRate)
	fmt.Fprintf(&b, "| 调用失败（已放行） | %d |\n| 模型命中 | %d |\n", stats.Errors, stats.Flagged)
	fmt.Fprintf(&b, "| 拦截 | %d（模型 %d，哈希预检 %d，关键词 %d） |\n", blocked, stats.Blocked, stats.HashBlocks, stats.KeywordBlocks)
	fmt.Fprintf(&b, "| 观察名单命中（未拦截） | %d |\n", stats.ObserveFlagged)
	fmt.Fprintf(&b, "| Jev 输入 token | %d（约 $%.2f） |\n\n", stats.InputTokens, cost)
	b.WriteString("### 命中最多的用户\n\n")
	if len(top) == 0 {
		b.WriteString("无\n")
	}
	for i, u := range top {
		fmt.Fprintf(&b, "%d. %s（ID %d）命中 %d 次，拦截 %d 次\n", i+1, u.UserEmail, u.UserID, u.Hits, u.Blocks)
	}
	fmt.Fprintf(&b, "\n### 推送\n\n命中 %d · 告警 %d · ops %d · 其他 %d\n",
		sent[string(PushCategoryHit)], sent[string(PushCategoryAlert)], sent[string(PushCategoryOps)],
		sent[string(PushCategoryNotice)]+sent[string(PushCategoryReport)])
	return pushMessage{category: PushCategoryReport, title: "【日报】内容审计 " + day.Format("01-02"), desp: b.String()}
}
