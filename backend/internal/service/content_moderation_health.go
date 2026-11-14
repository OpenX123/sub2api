package service

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	moderationHealthBuckets      = 5 // one-minute buckets => 5 minute window
	moderationHealthMinCalls     = 20
	moderationHealthFailureRatio = 0.2
	moderationAlertRepeatEvery   = 30 * time.Minute
)

type moderationAttemptOutcome struct {
	Success        bool
	HTTPStatus     int
	NoKeyAvailable bool
	Err            string
}

type moderationHealthBucket struct {
	minute   int64
	calls    int
	failures int
}

type moderationAlertState struct {
	firing   bool
	lastSent time.Time
}

type contentModerationHealth struct {
	mu      sync.Mutex
	buckets [moderationHealthBuckets]moderationHealthBucket
	alerts  map[ModerationAlertKind]*moderationAlertState
}

func (h *contentModerationHealth) observe(now time.Time, o moderationAttemptOutcome) []ModerationSystemAlert {
	h.mu.Lock()
	defer h.mu.Unlock()
	minute := now.Unix() / 60
	if !o.NoKeyAvailable {
		bucket := &h.buckets[minute%moderationHealthBuckets]
		if bucket.minute != minute {
			*bucket = moderationHealthBucket{minute: minute}
		}
		bucket.calls++
		if !o.Success {
			bucket.failures++
		}
	}
	var out []ModerationSystemAlert
	switch {
	case o.Success:
		out = append(out, h.resolve(now, ModerationAlertBalanceExhausted)...)
		out = append(out, h.resolve(now, ModerationAlertAllKeysFrozen)...)
	case o.HTTPStatus == http.StatusPaymentRequired:
		out = append(out, h.fire(now, ModerationAlertBalanceExhausted, "TypeSafe 返回 402，余额可能已用完")...)
	case o.NoKeyAvailable:
		if !h.state(ModerationAlertBalanceExhausted).firing {
			out = append(out, h.fire(now, ModerationAlertAllKeysFrozen, o.Err)...)
		}
	}
	if calls, failures := h.windowTotals(minute); calls >= moderationHealthMinCalls {
		ratio := float64(failures) / float64(calls)
		if ratio > moderationHealthFailureRatio {
			out = append(out, h.fire(now, ModerationAlertFailureRate, fmt.Sprintf("近 5 分钟审计失败率 %.0f%%（%d/%d）", ratio*100, failures, calls))...)
		} else {
			out = append(out, h.resolve(now, ModerationAlertFailureRate)...)
		}
	}
	return out
}

func (h *contentModerationHealth) windowTotals(minute int64) (calls int, failures int) {
	for _, bucket := range h.buckets {
		if bucket.minute <= minute && minute-bucket.minute < moderationHealthBuckets {
			calls += bucket.calls
			failures += bucket.failures
		}
	}
	return calls, failures
}

func (h *contentModerationHealth) state(kind ModerationAlertKind) *moderationAlertState {
	if h.alerts == nil {
		h.alerts = map[ModerationAlertKind]*moderationAlertState{}
	}
	st := h.alerts[kind]
	if st == nil {
		st = &moderationAlertState{}
		h.alerts[kind] = st
	}
	return st
}

func (h *contentModerationHealth) fire(now time.Time, kind ModerationAlertKind, detail string) []ModerationSystemAlert {
	st := h.state(kind)
	if st.firing && now.Sub(st.lastSent) < moderationAlertRepeatEvery {
		return nil
	}
	st.firing, st.lastSent = true, now
	return []ModerationSystemAlert{{Kind: kind, Detail: detail, At: now}}
}

func (h *contentModerationHealth) resolve(now time.Time, kind ModerationAlertKind) []ModerationSystemAlert {
	st := h.state(kind)
	if !st.firing {
		return nil
	}
	st.firing = false
	return []ModerationSystemAlert{{Kind: kind, Resolved: true, At: now}}
}

func (s *ContentModerationService) reportAttempt(o moderationAttemptOutcome) {
	if s == nil {
		return
	}
	alerts := s.health.observe(time.Now(), o)
	if s.notifier == nil {
		return
	}
	for _, alert := range alerts {
		s.notifier.NotifySystemAlert(context.Background(), alert)
	}
}
