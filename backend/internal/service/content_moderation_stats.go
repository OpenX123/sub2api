package service

import (
	"context"
	"log/slog"
	"time"
)

// ContentModerationDailyStats are per-day counters used by the daily report.
type ContentModerationDailyStats struct {
	Audits         int64 `json:"audits"`
	UpstreamCalls  int64 `json:"upstream_calls"`
	CacheHits      int64 `json:"cache_hits"`
	InputTokens    int64 `json:"input_tokens"`
	Errors         int64 `json:"errors"`
	Flagged        int64 `json:"flagged"`
	Blocked        int64 `json:"blocked"`
	ObserveFlagged int64 `json:"observe_flagged"`
	HashBlocks     int64 `json:"hash_blocks"`
	KeywordBlocks  int64 `json:"keyword_blocks"`
}

func (a ContentModerationDailyStats) Plus(b ContentModerationDailyStats) ContentModerationDailyStats {
	return ContentModerationDailyStats{
		Audits: a.Audits + b.Audits, UpstreamCalls: a.UpstreamCalls + b.UpstreamCalls, CacheHits: a.CacheHits + b.CacheHits,
		InputTokens: a.InputTokens + b.InputTokens, Errors: a.Errors + b.Errors, Flagged: a.Flagged + b.Flagged,
		Blocked: a.Blocked + b.Blocked, ObserveFlagged: a.ObserveFlagged + b.ObserveFlagged,
		HashBlocks: a.HashBlocks + b.HashBlocks, KeywordBlocks: a.KeywordBlocks + b.KeywordBlocks,
	}
}

type ContentModerationStatsStore interface {
	Add(ctx context.Context, day string, delta ContentModerationDailyStats) error
	Get(ctx context.Context, day string) (ContentModerationDailyStats, error)
}

var contentModerationReportLocation = loadContentModerationReportLocation()

func loadContentModerationReportLocation() *time.Location {
	if loc, err := time.LoadLocation("Asia/Shanghai"); err == nil {
		return loc
	}
	return time.FixedZone("CST", 8*3600)
}

// ContentModerationReportLocation is the fixed Beijing-time zone used for stats days and the daily report.
func ContentModerationReportLocation() *time.Location { return contentModerationReportLocation }

func ContentModerationStatsDay(t time.Time) string {
	return t.In(contentModerationReportLocation).Format("20060102")
}

func (s *ContentModerationService) SetStatsStore(store ContentModerationStatsStore) {
	if s != nil {
		s.statsStore = store
	}
}

const contentModerationStatsWriteTimeout = 200 * time.Millisecond

func (s *ContentModerationService) recordStats(delta ContentModerationDailyStats) {
	if s == nil || s.statsStore == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), contentModerationStatsWriteTimeout)
	defer cancel()
	if err := s.statsStore.Add(ctx, ContentModerationStatsDay(time.Now()), delta); err != nil {
		slog.Warn("content_moderation.stats_record_failed", "error", err)
	}
}
