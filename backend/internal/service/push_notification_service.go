package service

import (
	"context"
	"log/slog"
	"net/url"
	"sync"
	"time"
)

const (
	pushQueueSize         = 256
	pushPerMinuteLimit    = 50
	pushSendTimeout       = 15 * time.Second
	defaultPushRetryDelay = 2 * time.Second
	pushFlushInterval     = time.Minute
)

type PushSender interface {
	Send(ctx context.Context, sendKey, title, desp string) error
}

type PushNotificationStore interface {
	IncrSent(ctx context.Context, day, category string) (int64, error)
	GetSent(ctx context.Context, day string) (map[string]int64, error)
	TryOnce(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

// PushNotificationService delivers ServerChan pushes from a single background queue.
type PushNotificationService struct {
	settingRepo SettingRepository
	store       PushNotificationStore
	sender      PushSender
	now         func() time.Time
	retryDelay  time.Duration
	limiter     *slidingWindowLimiter
	queue       chan pushMessage
	stop        chan struct{}
	startOnce   sync.Once
	stopOnce    sync.Once
	wg          sync.WaitGroup
	hits        moderationHitAggregator
	accounts    PushAccountSource
	accountSeen accountErrorSeen
}

func NewPushNotificationService(settingRepo SettingRepository, store PushNotificationStore, sender PushSender) *PushNotificationService {
	return &PushNotificationService{
		settingRepo: settingRepo, store: store, sender: sender, now: time.Now,
		retryDelay: defaultPushRetryDelay, limiter: newSlidingWindowLimiter(pushPerMinuteLimit, time.Minute),
		queue: make(chan pushMessage, pushQueueSize), stop: make(chan struct{}),
	}
}

func pushDay(t time.Time) string { return ContentModerationStatsDay(t) }

// loadConfig never fails: on error it returns a disabled default so callers simply skip pushing.
func (s *PushNotificationService) loadConfig(ctx context.Context) *PushNotificationConfig {
	cfg, err := s.readConfig(ctx)
	if err != nil {
		slog.Warn("push_notification.config_load_failed", "error", err)
		return defaultPushNotificationConfig()
	}
	return cfg
}

func (s *PushNotificationService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go s.run()
	})
}

func (s *PushNotificationService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stop) })
	s.wg.Wait()
}

func (s *PushNotificationService) run() {
	defer s.wg.Done()
	ticker := time.NewTicker(pushFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case msg := <-s.queue:
			ctx, cancel := context.WithTimeout(context.Background(), 2*pushSendTimeout+defaultPushRetryDelay)
			s.deliver(ctx, msg)
			cancel()
		case <-ticker.C:
			s.flushHitWindows(context.Background())
			s.runAccountCheck()
		}
	}
}

// enqueue never blocks callers on the request path; a full queue drops the message.
func (s *PushNotificationService) enqueue(msg pushMessage) bool {
	if s == nil {
		return false
	}
	select {
	case s.queue <- msg:
		return true
	default:
		slog.Warn("push_notification.queue_full", "category", msg.category)
		return false
	}
}

func (s *PushNotificationService) deliver(ctx context.Context, msg pushMessage) {
	cfg := s.loadConfig(ctx)
	if !cfg.Enabled || cfg.SendKey == "" || s.sender == nil {
		return
	}
	if s.store != nil {
		count, err := s.store.IncrSent(ctx, pushDay(s.now()), string(msg.category))
		if err != nil {
			slog.Warn("push_notification.counter_failed", "error", err)
		} else if msg.category == PushCategoryHit && count > int64(cfg.HitDailyCap) {
			if count == int64(cfg.HitDailyCap)+1 {
				s.deliver(ctx, formatHitCapNotice(cfg.HitDailyCap))
			}
			return
		}
	}
	if !s.limiter.Allow(s.now()) {
		slog.Warn("push_notification.rate_limited", "category", msg.category)
		return
	}
	if err := s.send(ctx, cfg.SendKey, msg); err == nil {
		return
	}
	if s.retryDelay > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(s.retryDelay):
		}
	}
	if err := s.send(ctx, cfg.SendKey, msg); err != nil {
		slog.Warn("push_notification.send_failed", "category", msg.category, "error", err)
	}
}

func (s *PushNotificationService) send(ctx context.Context, key string, msg pushMessage) error {
	ctx, cancel := context.WithTimeout(ctx, pushSendTimeout)
	defer cancel()
	return s.sender.Send(ctx, key, msg.title, msg.desp)
}

func (s *PushNotificationService) NotifySystemAlert(_ context.Context, alert ModerationSystemAlert) {
	s.enqueue(formatSystemAlert(alert))
}

// NotifyModerationHit pushes the first hit of a user's merge window immediately and
// folds later hits into a summary sent when the window closes.
func (s *PushNotificationService) NotifyModerationHit(ctx context.Context, ev ModerationHitEvent) {
	if s == nil {
		return
	}
	cfg := s.loadConfig(ctx)
	if !cfg.Enabled {
		return
	}
	window := time.Duration(cfg.HitWindowMinutes) * time.Minute
	immediate, expired := s.hits.record(ev, window)
	if expired != nil {
		s.enqueue(formatHitSummary(*expired))
	}
	if immediate {
		s.enqueue(formatModerationHit(ev, s.adminLogLink(ctx, ev.RequestID)))
	}
}

func (s *PushNotificationService) flushHitWindows(ctx context.Context) {
	cfg := s.loadConfig(ctx)
	window := time.Duration(cfg.HitWindowMinutes) * time.Minute
	for _, sum := range s.hits.flush(s.now(), window) {
		s.enqueue(formatHitSummary(sum))
	}
}

func (s *PushNotificationService) runAccountCheck() {
	ctx, cancel := context.WithTimeout(context.Background(), pushSendTimeout)
	defer cancel()
	s.checkAccountErrors(ctx)
}

func (s *PushNotificationService) adminLogLink(ctx context.Context, requestID string) string {
	if requestID == "" {
		return ""
	}
	return s.adminPageLink(ctx, "/admin/risk-control?search="+url.QueryEscape(requestID))
}

// NotifyOpsAlert forwards a P0 ops alert when the ops push switch is on.
func (s *PushNotificationService) NotifyOpsAlert(ctx context.Context, rule *OpsAlertRule, event *OpsAlertEvent) {
	if s == nil || rule == nil || event == nil {
		return
	}
	if !s.loadConfig(ctx).OpsCriticalEnabled {
		return
	}
	s.enqueue(formatOpsAlert(rule, event))
}
