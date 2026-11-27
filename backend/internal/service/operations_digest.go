package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/feishu"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const digestCheckpoint = "operations_digest_last_slot"

func (s *ContentModerationDailyReportService) sendOperationsDigest(ctx context.Context, cfg *PushNotificationConfig, now time.Time) error {
	slot := time.Date(now.Year(), now.Month(), now.Day(), now.Hour()/3*3, 0, 0, 0, now.Location())
	token := slot.Format(time.RFC3339)
	previous, err := s.push.settingRepo.GetValue(ctx, digestCheckpoint)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return err
	}
	if previous == token {
		return nil
	}
	locked, err := s.pushStore.TryOnce(ctx, "push_notification:digest:"+token, 2*time.Minute)
	if err != nil || !locked {
		return err
	}
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	card, err := s.reports.OperationsDigest(ctx, slot, now, day)
	if err != nil {
		return err
	}
	var accounts []Account
	for page := 1; ; page++ {
		batch, info, err := s.ops.accountRepo.ListWithFilters(ctx, pagination.PaginationParams{Page: page, PageSize: 200}, PlatformAnthropic, "", "", "", 0, "")
		if err != nil {
			return err
		}
		accounts = append(accounts, batch...)
		if len(batch) == 0 || (info != nil && int64(len(accounts)) >= info.Total) {
			break
		}
	}
	ids := make([]int64, 0, len(accounts))
	var ready, limited, cooling, broken, capacity, current int
	for _, a := range accounts {
		ids = append(ids, a.ID)
		if a.IsSchedulable() {
			ready++
			capacity += a.Concurrency
		} else if a.Status == StatusError {
			broken++
		} else if a.RateLimitResetAt != nil && a.RateLimitResetAt.After(now) {
			limited++
		} else if (a.OverloadUntil != nil && a.OverloadUntil.After(now)) || (a.TempUnschedulableUntil != nil && a.TempUnschedulableUntil.After(now)) {
			cooling++
		}
	}
	load, err := s.ops.concurrencyService.GetAccountConcurrencyBatch(ctx, ids)
	if err != nil {
		return err
	}
	for _, n := range load {
		current += n
	}
	card.Title = os.Getenv("OPERATIONS_DIGEST_TITLE")
	if card.Title == "" {
		card.Title = "Sub2API · 运营速报"
	}
	card.Window = fmt.Sprintf("%s–%s · 北京时间", slot.Add(-3*time.Hour).Format("01-02 15:04"), slot.Format("15:04"))
	card.Pool = fmt.Sprintf("%d / %d", ready, len(accounts))
	card.Concurrency = fmt.Sprintf("%d / %d", current, capacity)
	card.PoolDetail = fmt.Sprintf("限流 %d · 冷却 %d · 异常 %d", limited, cooling, broken)
	sender, ok := s.push.sender.(interface {
		SendDigestCard(context.Context, string, feishu.DigestCard) error
	})
	if !ok {
		return errors.New("digest card sender unavailable")
	}
	card.Suggestions = s.ops.digestAnalysis(ctx, card)
	if err := sender.SendDigestCard(ctx, cfg.SendKey, *card); err != nil {
		return err
	}
	slog.Info("operations_digest.sent", "slot", token, "trend_points", len(card.Trend),
		"risk_examples", len(card.Examples), "suggestions", len(card.Suggestions))
	return s.push.settingRepo.Set(ctx, digestCheckpoint, token)
}
