package service

import (
	"time"
)

// SubscriptionCacheData represents cached subscription data
type SubscriptionCacheData struct {
	Status       string
	ExpiresAt    time.Time
	DailyUsage   float64
	WeeklyUsage  float64
	MonthlyUsage float64
	// Usage5h 与 Window5hStart 描述当前 5h 窗口；窗口已过期时 Usage5h 不再生效。
	Usage5h       float64
	Window5hStart *time.Time
	Version       int64
}
