package service

import (
	"sort"
	"sync"
	"time"
)

type moderationHitWindow struct {
	start      time.Time
	firstSent  bool
	extra      int
	categories map[string]int
	email      string
	observe    bool
}

type moderationHitSummary struct {
	UserID      int64
	UserEmail   string
	Count       int
	TopCategory string
	Observe     bool
	WindowStart time.Time
	Window      time.Duration
}

// moderationHitAggregator keeps one in-memory merge window per user (per instance).
type moderationHitAggregator struct {
	mu      sync.Mutex
	windows map[int64]*moderationHitWindow
}

// record returns whether ev should be pushed immediately, plus the summary of an
// expired window that ev displaced (if it had merged hits).
func (a *moderationHitAggregator) record(ev ModerationHitEvent, window time.Duration) (bool, *moderationHitSummary) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.windows == nil {
		a.windows = map[int64]*moderationHitWindow{}
	}
	var expired *moderationHitSummary
	w := a.windows[ev.UserID]
	if w != nil && ev.At.Sub(w.start) >= window {
		expired = w.summary(ev.UserID, window)
		w = nil
	}
	if w == nil {
		w = &moderationHitWindow{start: ev.At, categories: map[string]int{}}
		a.windows[ev.UserID] = w
	}
	w.email, w.observe = ev.UserEmail, ev.Observe
	if !w.firstSent && ev.Action != ContentModerationActionHashBlock {
		w.firstSent = true
		return true, expired
	}
	w.extra++
	w.categories[ev.Category]++
	return false, expired
}

func (a *moderationHitAggregator) flush(now time.Time, window time.Duration) []moderationHitSummary {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []moderationHitSummary
	for userID, w := range a.windows {
		if now.Sub(w.start) < window {
			continue
		}
		if sum := w.summary(userID, window); sum != nil {
			out = append(out, *sum)
		}
		delete(a.windows, userID)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out
}

func (w *moderationHitWindow) summary(userID int64, window time.Duration) *moderationHitSummary {
	if w.extra == 0 {
		return nil
	}
	top, topCount := "", -1
	for category, count := range w.categories {
		if count > topCount || (count == topCount && category < top) {
			top, topCount = category, count
		}
	}
	return &moderationHitSummary{UserID: userID, UserEmail: w.email, Count: w.extra, TopCategory: top, Observe: w.observe, WindowStart: w.start, Window: window}
}
