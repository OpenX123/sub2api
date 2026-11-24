//go:build unit

package service

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type anthropicWindowLimitRepo struct {
	mockAccountRepoForGemini
	rateLimitCalls          int
	tempUnschedCalls        int
	lastRateLimitReset      time.Time
	modelRateLimitCalls     int
	lastModelRateLimitScope string
	lastModelRateLimitReset time.Time
	sessionWindowCalls      int
	lastExtraUpdates        map[string]any
}

func (r *anthropicWindowLimitRepo) SetRateLimited(_ context.Context, _ int64, resetAt time.Time) error {
	r.rateLimitCalls++
	r.lastRateLimitReset = resetAt
	return nil
}

func (r *anthropicWindowLimitRepo) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, _ string) error {
	r.tempUnschedCalls++
	return nil
}

func (r *anthropicWindowLimitRepo) SetModelRateLimit(_ context.Context, _ int64, scope string, resetAt time.Time, _ ...string) error {
	r.modelRateLimitCalls++
	r.lastModelRateLimitScope = scope
	r.lastModelRateLimitReset = resetAt
	return nil
}

func (r *anthropicWindowLimitRepo) UpdateSessionWindow(_ context.Context, _ int64, _, _ *time.Time, _ string) error {
	r.sessionWindowCalls++
	return nil
}

func (r *anthropicWindowLimitRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.lastExtraUpdates = updates
	return nil
}

func TestHandleUpstreamError_AnthropicWindowLimitPreemptsTempUnschedRule(t *testing.T) {
	resetAt := time.Now().Add(3 * time.Hour).Truncate(time.Second)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.02")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(resetAt.Unix(), 10))

	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{
		ID:       42,
		Type:     AccountTypeOAuth,
		Platform: PlatformAnthropic,
		Credentials: map[string]any{
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules": []any{
				map[string]any{
					"error_code":       float64(http.StatusTooManyRequests),
					"keywords":         []any{"rate limit"},
					"duration_minutes": float64(10),
				},
			},
		},
	}

	svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusTooManyRequests,
		headers,
		[]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed your account's rate limit. Please try again later."}}`),
	)

	require.Zero(t, repo.tempUnschedCalls, "official Anthropic window limits should not be shortened by local temp-unsched rules")
	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, resetAt, repo.lastRateLimitReset)
}

// fable429Headers 构造 7d_oi（Fable 专属 7d 窗口）触发 429 的完整响应头，
// 数值取自真实抓包（5h/7d 均 allowed，仅 7d_oi rejected）。
func fable429Headers(reset5h, resetOI time.Time) http.Header {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-5h-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.41")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(resetOI.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.56")
	headers.Set("anthropic-ratelimit-unified-7d_oi-reset", strconv.FormatInt(resetOI.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d_oi-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-7d_oi-surpassed-threshold", "1.0")
	headers.Set("anthropic-ratelimit-unified-7d_oi-utilization", "1.0")
	headers.Set("anthropic-ratelimit-unified-fallback-percentage", "0.5")
	headers.Set("anthropic-ratelimit-unified-overage-disabled-reason", "org_level_disabled")
	headers.Set("anthropic-ratelimit-unified-overage-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-representative-claim", "seven_day_overage_included")
	headers.Set("anthropic-ratelimit-unified-reset", strconv.FormatInt(resetOI.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-status", "rejected")
	return headers
}
func TestHandleUpstreamError_Anthropic7dOiOnlyMarksModelRateLimit(t *testing.T) {
	now := time.Now()
	reset5h := anthropicResetOffset(now, 2*time.Hour)
	resetOI := anthropicResetOffset(now, 80*time.Hour)
	headers := fable429Headers(reset5h, resetOI)

	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{
		ID:       42,
		Type:     AccountTypeOAuth,
		Platform: PlatformAnthropic,
		Credentials: map[string]any{
			"temp_unschedulable_enabled": true,
			"temp_unschedulable_rules": []any{
				map[string]any{
					"error_code":       float64(http.StatusTooManyRequests),
					"keywords":         []any{"rate limit"},
					"duration_minutes": float64(10),
				},
			},
		},
	}

	shouldDisable := svc.HandleUpstreamError(
		context.Background(),
		account,
		http.StatusTooManyRequests,
		headers,
		[]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"This request would exceed your account's rate limit. Please try again later."}}`),
		"claude-fable-5",
	)

	require.False(t, shouldDisable)
	require.Zero(t, repo.rateLimitCalls, "7d_oi (Fable-only) window must not mark the whole account rate limited")
	require.Zero(t, repo.tempUnschedCalls, "7d_oi window must not trigger local temp-unsched rules")
	require.Zero(t, repo.sessionWindowCalls, "7d_oi window must not rewrite the 5h session window as rejected")
	require.Equal(t, 1, repo.modelRateLimitCalls)
	require.Equal(t, anthropicFableRateLimitKey, repo.lastModelRateLimitScope)
	require.Equal(t, resetOI, repo.lastModelRateLimitReset)

	// 429 响应头也要被动采样，避免 7d F 进度条在限流期内冻结在旧值
	require.NotNil(t, repo.lastExtraUpdates)
	require.Equal(t, 1.0, repo.lastExtraUpdates["passive_usage_7d_oi_utilization"])
	require.Equal(t, resetOI.Unix(), repo.lastExtraUpdates["passive_usage_7d_oi_reset"])
	require.Equal(t, 0.41, repo.lastExtraUpdates["session_window_utilization"])
}

func TestHandleUpstreamError_AnthropicFableCreditsRequiredOnlyMarksModelRateLimit(t *testing.T) {
	resetAt := time.Now().Add(29 * 24 * time.Hour).Truncate(time.Second)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-reset", strconv.FormatInt(resetAt.Unix(), 10))

	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{ID: 42, Type: AccountTypeOAuth, Platform: PlatformAnthropic}
	body := []byte(`{"type":"error","error":{"details":{"error_code":"credits_required","model":"claude-fable-5","disabled_reason":"org_level_disabled"},"message":"Usage credits are required for this model."}}`)

	shouldDisable := svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, body, "claude-fable-5")

	require.False(t, shouldDisable)
	require.Zero(t, repo.rateLimitCalls, "Fable entitlement failure must not rate limit the whole account")
	require.Zero(t, repo.sessionWindowCalls, "Fable entitlement failure must not rewrite the shared session window")
	require.Equal(t, 1, repo.modelRateLimitCalls)
	require.Equal(t, anthropicFableRateLimitKey, repo.lastModelRateLimitScope)
	require.Equal(t, resetAt, repo.lastModelRateLimitReset)
}

func TestHandleUpstreamError_AnthropicFableCreditsRequiredFallsBackToRequestedModel(t *testing.T) {
	startedAt := time.Now()
	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{ID: 42, Type: AccountTypeOAuth, Platform: PlatformAnthropic}
	body := []byte(`{"type":"error","error":{"details":{"error_code":"credits_required"},"message":"Usage credits are required for this model."}}`)

	svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, http.Header{}, body, "claude-fable-5[1m]")

	require.Zero(t, repo.rateLimitCalls)
	require.Equal(t, 1, repo.modelRateLimitCalls)
	require.Equal(t, anthropicFableRateLimitKey, repo.lastModelRateLimitScope)
	require.True(t, repo.lastModelRateLimitReset.After(startedAt))
}

func TestHandleUpstreamError_AnthropicNonFableCreditsRequiredKeepsLegacyBehavior(t *testing.T) {
	resetAt := anthropicResetOffset(time.Now(), 2*time.Hour)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-reset", strconv.FormatInt(resetAt.Unix(), 10))

	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{ID: 42, Type: AccountTypeOAuth, Platform: PlatformAnthropic}
	body := []byte(`{"type":"error","error":{"details":{"error_code":"credits_required","model":"claude-opus-5"},"message":"Usage credits are required for this model."}}`)

	svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, body, "claude-opus-5")

	require.Zero(t, repo.modelRateLimitCalls)
	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, resetAt, repo.lastRateLimitReset)
}

func TestHandleUpstreamError_AnthropicSharedWindowStillWinsWithFableCreditsRequired(t *testing.T) {
	now := time.Now()
	reset5h := anthropicResetOffset(now, 2*time.Hour)
	reset7d := anthropicResetOffset(now, 80*time.Hour)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.0")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(reset7d.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-reset", strconv.FormatInt(reset5h.Unix(), 10))

	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{ID: 42, Type: AccountTypeOAuth, Platform: PlatformAnthropic}
	body := []byte(`{"type":"error","error":{"details":{"error_code":"credits_required","model":"claude-fable-5"}}}`)

	svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, body, "claude-fable-5")

	require.Equal(t, 1, repo.modelRateLimitCalls, "Fable-specific failure should still be recorded")
	require.Equal(t, 1, repo.rateLimitCalls, "an explicitly rejected shared window must still rate limit the account")
	require.Equal(t, reset5h, repo.lastRateLimitReset)
}

func TestHandleUpstreamError_Anthropic5hWindowStillWinsOver7dOi(t *testing.T) {
	// 5h 窗口 rejected 时必须仍按账号级限流处理（用 5h reset），同时记录 Fable 模型限流。
	now := time.Now()
	reset5h := anthropicResetOffset(now, 2*time.Hour)
	resetOI := anthropicResetOffset(now, 80*time.Hour)
	headers := fable429Headers(reset5h, resetOI)
	headers.Set("anthropic-ratelimit-unified-5h-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.0")

	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{ID: 42, Type: AccountTypeOAuth, Platform: PlatformAnthropic}

	svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, nil, "claude-fable-5")

	require.Equal(t, 1, repo.rateLimitCalls, "exhausted 5h window must still rate limit the account")
	require.Equal(t, reset5h, repo.lastRateLimitReset)
	require.Equal(t, 1, repo.modelRateLimitCalls)
	require.Equal(t, anthropicFableRateLimitKey, repo.lastModelRateLimitScope)
}

func TestHandleUpstreamError_AnthropicAccountWindowStillWinsOver7dOi(t *testing.T) {
	// 7d 窗口真超限时必须仍按账号级限流处理，同时记录 Fable 模型限流。
	now := time.Now()
	reset5h := anthropicResetOffset(now, 2*time.Hour)
	resetOI := anthropicResetOffset(now, 80*time.Hour)
	headers := fable429Headers(reset5h, resetOI)
	headers.Set("anthropic-ratelimit-unified-7d-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.02")

	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{ID: 42, Type: AccountTypeOAuth, Platform: PlatformAnthropic}

	svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, nil, "claude-fable-5")

	require.Equal(t, 1, repo.rateLimitCalls, "exhausted 7d window must still rate limit the account")
	require.Equal(t, resetOI, repo.lastRateLimitReset)
	require.Equal(t, 1, repo.modelRateLimitCalls, "Fable model rate limit should also be recorded")
	require.Equal(t, anthropicFableRateLimitKey, repo.lastModelRateLimitScope)
}

func TestHandleUpstreamError_Anthropic429Without7dOiKeepsLegacyBehavior(t *testing.T) {
	// 无 7d_oi 头、5h/7d 均未超限的 429：保持旧行为（按较早 reset 标记账号限流）。
	now := time.Now()
	reset5h := anthropicResetOffset(now, 2*time.Hour)
	reset7d := anthropicResetOffset(now, 80*time.Hour)

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-5h-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.41")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(reset7d.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.56")

	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{ID: 42, Type: AccountTypeOAuth, Platform: PlatformAnthropic}

	svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, nil, "claude-fable-5")

	require.Zero(t, repo.modelRateLimitCalls, "no 7d_oi signal → no model rate limit")
	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, reset5h, repo.lastRateLimitReset, "legacy path picks the sooner reset")
	require.Equal(t, 1, repo.sessionWindowCalls)
}

// 长上下文 "Usage credits are required" 的 429 只带聚合头，且重置点是月度 claim 的次月 1 日。
// 不能把整个账号封到月初，也不能把它反推成一个远期的 5h 窗口，应走秒级兜底冷却。
func TestHandleUpstreamError_AnthropicAggregateResetBeyondWindowUsesFallback(t *testing.T) {
	resetAt := time.Now().Add(17*24*time.Hour + 15*time.Hour).Truncate(time.Second)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-reset", strconv.FormatInt(resetAt.Unix(), 10))

	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{ID: 155, Type: AccountTypeOAuth, Platform: PlatformAnthropic}
	body := []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Usage credits are required for long context requests."}}`)

	before := time.Now()
	svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, body, "claude-sonnet-4-6")

	require.Equal(t, 1, repo.rateLimitCalls)
	require.True(t, repo.lastRateLimitReset.Before(before.Add(time.Hour)), "out-of-range aggregate reset must fall back to a short cooldown, got %v", repo.lastRateLimitReset)
	require.Zero(t, repo.sessionWindowCalls, "out-of-range aggregate reset must not rewrite the 5h session window")
}

// 聚合头落在 7d 量级内时仍按账号限流，但不应被写成"5 小时窗口"。
func TestHandleUpstreamError_AnthropicAggregateReset7dScaleSkipsSessionWindow(t *testing.T) {
	resetAt := anthropicResetOffset(time.Now(), 3*24*time.Hour)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-reset", strconv.FormatInt(resetAt.Unix(), 10))

	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{ID: 42, Type: AccountTypeOAuth, Platform: PlatformAnthropic}

	svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Error"}}`))

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, resetAt, repo.lastRateLimitReset)
	require.Zero(t, repo.sessionWindowCalls)
}

// 聚合头落在 5h 量级内时保持原行为：账号限流并反推 session window。
func TestHandleUpstreamError_AnthropicAggregateReset5hScaleUpdatesSessionWindow(t *testing.T) {
	resetAt := anthropicResetOffset(time.Now(), 2*time.Hour)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-reset", strconv.FormatInt(resetAt.Unix(), 10))

	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{ID: 42, Type: AccountTypeOAuth, Platform: PlatformAnthropic}

	svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Error"}}`))

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, resetAt, repo.lastRateLimitReset)
	require.Equal(t, 1, repo.sessionWindowCalls)
}

// 月度额度 claim 的重置点落在次月 1 日 00:00 UTC。它落在 8 天上限之内（月末 8 天内
// 触发时距月初不足 8 天），但根本不是账号的 5h/7d 窗口——账号自身 7d 利用率只有
// 78%~93%。照单全收会把账号封到月初（实测 6 天 10 小时），必须改走秒级兜底。
//
// 用真实的生产日期构造：2026-12-01 00:00 UTC 距 2026-11-24 约 6.4 天。
func TestHandleUpstreamError_AnthropicMonthStartAggregateResetUsesFallback(t *testing.T) {
	// 生产现场：2026-11-24 收到长上下文 429，聚合头指向 2026-12-01 00:00 UTC。
	monthStart := time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC)
	now := monthStart.Add(-6*24*time.Hour - 10*time.Hour) // 距月初 6d10h

	// 聚合头解析层必须拒绝这个重置点（它在 8 天上限内，但不是账号窗口）。
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-reset", strconv.FormatInt(monthStart.Unix(), 10))
	_, ok := parseAnthropicAggregateReset(headers, now)
	require.False(t, ok, "month-start reset must be rejected by the aggregate parser")

	// 端到端：账号不得被封到月初，而应落到秒级兜底冷却。
	repo := &anthropicWindowLimitRepo{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	account := &Account{ID: 153, Type: AccountTypeOAuth, Platform: PlatformAnthropic}
	body := []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Usage credits are required for long context requests."}}`)

	before := time.Now()
	svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests, headers, body, "claude-sonnet-4-6")

	require.Equal(t, 1, repo.rateLimitCalls)
	require.True(t, repo.lastRateLimitReset.Before(before.Add(time.Hour)),
		"month-start aggregate reset must fall back to a short cooldown, got %v", repo.lastRateLimitReset)
	require.Zero(t, repo.sessionWindowCalls, "month-start aggregate reset must not rewrite the 5h session window")
}

func TestIsMonthStartReset(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want bool
	}{
		{"exact month start", time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC), true},
		{"month start plus 30m", time.Date(2026, time.December, 1, 0, 30, 0, 0, time.UTC), true},
		{"month start minus 30m", time.Date(2026, time.November, 30, 23, 30, 0, 0, time.UTC), true},
		{"month start plus 2h", time.Date(2026, time.December, 1, 2, 0, 0, 0, time.UTC), false},
		{"mid month", time.Date(2026, time.December, 15, 0, 0, 0, 0, time.UTC), false},
		{"seven day window reset", time.Date(2026, time.December, 1, 3, 0, 0, 0, time.UTC), false},
		{"non utc input still matches", time.Date(2026, time.December, 1, 8, 0, 0, 0, time.FixedZone("CST", 8*3600)), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isMonthStartReset(tc.in))
		})
	}
}
