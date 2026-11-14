package service

import (
	"context"
	"time"
)

type ModerationAlertKind string

const (
	ModerationAlertBalanceExhausted ModerationAlertKind = "balance_exhausted"
	ModerationAlertAllKeysFrozen    ModerationAlertKind = "all_keys_frozen"
	ModerationAlertFailureRate      ModerationAlertKind = "failure_rate"
)

// ModerationSystemAlert is a firing (Resolved=false) or recovery (Resolved=true) event for the audit engine.
type ModerationSystemAlert struct {
	Kind     ModerationAlertKind
	Resolved bool
	Detail   string
	At       time.Time
}

// ModerationHitEvent describes one flagged audit. It never carries the audited text.
type ModerationHitEvent struct {
	UserID    int64
	UserEmail string
	GroupName string
	Model     string
	Category  string
	Score     float64
	Action    string
	Observe   bool
	InputHash string
	RequestID string
	At        time.Time
}

type ContentModerationNotifier interface {
	NotifyModerationHit(ctx context.Context, ev ModerationHitEvent)
	NotifySystemAlert(ctx context.Context, alert ModerationSystemAlert)
}

func (s *ContentModerationService) SetNotifier(n ContentModerationNotifier) {
	if s != nil {
		s.notifier = n
	}
}
