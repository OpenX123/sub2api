# 内容审计观察名单 + Server酱推送 + Jev 降本 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 风控白名单用户在内容审计上照审照拦（通过可清空的"审计观察名单"灰度），审计命中/系统告警/日报通过 Server酱 推到微信，同时把 Jev 调用量降低 50% 以上且不改变任何拦截决策。

**Architecture:** 在 `ContentModerationService` 上通过 setter 注入三个新协作者：统计存储（Redis 日计数）、结果缓存（Redis，24h）、通知器（`ContentModerationNotifier`）。新增 `PushNotificationService`（Server酱 发送队列、每日上限、按用户合并窗口、ops P0 转发），以及 `ContentModerationDailyReportService`（每分钟 tick，北京时间 09:00 发前一天日报，Redis SETNX 防重）。审计观察名单作为 `content_moderation_config.observe_user_ids` 字段，取代内容审计对 `cyber_policy_user_allowlist` 的读取；网关入口把审计调用挪到选定账号之后、首次转发之前。

**Tech Stack:** Go 1.27（Gin、Google Wire、go-redis v9、testify、miniredis）、PostgreSQL、Vue 3 + TypeScript + Vitest（pnpm）。

**Spec:** `openspec/changes/moderation-observe-push-cost/proposal.md`（决策 D1–D12、验收标准、上线步骤）。

## Global Constraints

- 推送内容不得包含违规原文（input_excerpt）和 SendKey；SendKey 不写日志、不出现在任何 GET 响应里（只返回 `send_key_configured` 和 `send_key_masked`）。
- Server酱 只支持 Turbo（SendKey 以 `SCT` 开头），接口 `POST https://sctapi.ftqq.com/{SendKey}.send`，表单字段 `title`、`desp`，返回 `code == 0` 表示成功；标题最多 32 个字符，不能有换行。
- 推送失败最多重试 1 次；全局每分钟最多 50 条；命中类推送每天上限默认 300 条（可配 1–1000）。
- 日报时区固定 `Asia/Shanghai`（加载失败时退回 UTC+8 固定时区），默认 09:00。
- Jev 故障时保持 fail-open；任何改动不得改变非观察名单用户的拦截决策。
- 新迁移文件名用 fork 后缀约定：`248a_*.sql`（当前最大上游迁移是 248）。
- 后端测试：`cd backend && go test ./internal/... -run <Name> -count=1`（`content_moderation*` 测试文件没有 build tag）；全量：`go test -tags=unit ./...`；lint：`golangci-lint run ./...`（v2.13）。
- Wire：改了 provider 之后运行 `cd backend && go generate ./cmd/server`，并提交 `cmd/server/wire_gen.go`。
- 前端：`cd frontend && pnpm vitest run <spec>`；新增 i18n key 必须 zh/en 同时加（`localeKeyCompleteness.spec.ts` 会检查）。
- 提交信息用 conventional commits（中文描述），结尾加 `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`。

## File Structure

| 文件 | 职责 | 任务 |
|---|---|---|
| `backend/internal/service/content_moderation_engines.go` | `ContentModerationEngineMeta` 增加 `input_tokens`、`cached` | 1 |
| `backend/internal/service/content_moderation_typesafe.go` | 记录 Jev usage | 1 |
| `backend/internal/service/content_moderation_stats.go`（新） | 日统计结构、接口、时区工具、`recordStats` | 2 |
| `backend/internal/repository/content_moderation_stats_store.go`（新） | Redis HINCRBY 日计数 | 2 |
| `backend/internal/service/content_moderation.go` | 402 冻结、真实错误、统计埋点、健康上报、命中通知、观察名单、缓存接入 | 2,3,4,8,10,11 |
| `backend/internal/service/content_moderation_notify.go`（新） | `ContentModerationNotifier` 接口、事件类型 | 4 |
| `backend/internal/service/content_moderation_health.go`（新） | 失败率窗口 + 告警状态机 | 4 |
| `backend/internal/pkg/serverchan/client.go`（新） | Server酱 Turbo 客户端 | 5 |
| `backend/internal/service/push_notification_config.go`（新） | 推送配置、校验、视图、Get/Update/SendTest | 6 |
| `backend/internal/service/push_notification_service.go`（新） | 发送队列、上限、重试、限流、通知器实现 | 6,7,8,9 |
| `backend/internal/service/push_notification_format.go`（新） | 各类消息的标题/正文格式化 | 7,8,9 |
| `backend/internal/service/moderation_hit_aggregator.go`（新） | 按用户合并窗口 | 8 |
| `backend/internal/repository/push_notification_store.go`（新） | Redis 发送计数、SETNX | 6 |
| `backend/internal/handler/admin/push_notification_handler.go`（新） | 推送配置 GET/PUT/测试 | 6 |
| `backend/internal/service/ops_alert_evaluator_service.go` | P0 告警转发 | 9 |
| `backend/migrations/248a_content_moderation_observe_users.sql`（新） | 白名单复制进观察名单 | 10 |
| `backend/internal/service/content_moderation_verdict_cache.go`（新） | 缓存接口、key、元数据 | 11 |
| `backend/internal/repository/content_moderation_verdict_cache.go`（新） | Redis 缓存实现 | 11 |
| `backend/internal/handler/*.go`（网关入口） | 审计后移 | 12 |
| `backend/internal/service/content_moderation_daily_report.go`（新） | 日报服务 | 13 |
| `backend/internal/service/wire.go`、`backend/internal/repository/wire.go`、`backend/internal/handler/wire.go`、`backend/internal/handler/handler.go`、`backend/cmd/server/wire.go`、`backend/cmd/server/wire_gen_test.go` | 依赖注入与关停 | 2,6,7,9,11,13 |
| `frontend/src/api/admin/riskControl.ts` | 新字段、推送 API | 14,15 |
| `frontend/src/views/admin/RiskControlView.vue` | 观察名单、缓存 TTL、推送 tab、search 预填 | 14,15 |
| `frontend/src/views/admin/riskControl/RiskControlPushSettings.vue`（新） | 推送设置面板 | 15 |
| `frontend/src/i18n/locales/{zh,en}/admin/channels.ts` | `admin.riskControl.*` 文案 | 14,15 |

---

### Task 1: 记录 Jev token 用量

**Files:**
- Modify: `backend/internal/service/content_moderation_engines.go:38-43`（`ContentModerationEngineMeta`）
- Modify: `backend/internal/service/content_moderation_typesafe.go:78-80`
- Test: `backend/internal/service/content_moderation_typesafe_usage_test.go`（新）

**Interfaces:**
- Produces: `ContentModerationEngineMeta.InputTokens int`（json `input_tokens,omitempty`）、`ContentModerationEngineMeta.Cached bool`（json `cached,omitempty`）；测试辅助函数 `typeSafeAnswersBody(t *testing.T, scores map[string]float64, inputTokens int) []byte`（后续任务的测试复用）。

- [ ] **Step 1: 写失败的测试**

```go
package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// typeSafeAnswersBody builds a successful System One response covering all 13 categories.
func typeSafeAnswersBody(t *testing.T, scores map[string]float64, inputTokens int) []byte {
	t.Helper()
	answers := map[string]any{}
	for _, category := range ContentModerationCategories() {
		answers[category] = map[string]any{"type": "noul", "noul": scores[category]}
	}
	raw, err := json.Marshal(map[string]any{
		"model":   "jev-1.14.0",
		"answers": answers,
		"usage":   map[string]any{"input_tokens": inputTokens, "output_tokens": 13},
	})
	require.NoError(t, err)
	return raw
}

func newTypeSafeTestConfig(baseURL string) *ContentModerationConfig {
	cfg := defaultContentModerationConfig()
	cfg.Engine = ContentModerationEngineTypeSafe
	cfg.BaseURL, cfg.Model = baseURL, "jev-1.14.0"
	cfg.APIKeys = []string{"ts-test-key"}
	cfg.TimeoutMS, cfg.RetryCount = 2000, 2
	return cfg
}

func TestTypeSafeModerationRecordsInputTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(typeSafeAnswersBody(t, map[string]float64{"sexual": 0.1}, 1234))
	}))
	defer server.Close()
	svc := &ContentModerationService{httpClient: server.Client(), keyHealth: map[string]*contentModerationKeyHealth{}}

	result, err := svc.callModerationOnceWithInput(context.Background(), newTypeSafeTestConfig(server.URL), "ts-test-key", "需要审核的文字", nil)

	require.NoError(t, err)
	require.Equal(t, 1234, result.EngineMeta.InputTokens)
	require.False(t, result.EngineMeta.Cached)
	raw, err := json.Marshal(result.EngineMeta)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"input_tokens":1234`)
	require.NotContains(t, string(raw), `"cached"`)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test ./internal/service -run TestTypeSafeModerationRecordsInputTokens -count=1`
Expected: 编译失败，`result.EngineMeta.InputTokens undefined`

- [ ] **Step 3: 最小实现**

`content_moderation_engines.go` 中把结构体改成：

```go
type ContentModerationEngineMeta struct {
	Engine        string `json:"engine"`
	Model         string `json:"model"`
	RulesVersion  string `json:"rules_version"`
	SkippedImages int    `json:"skipped_images"`
	// InputTokens is the billable input token count reported by the engine (TypeSafe only).
	InputTokens int `json:"input_tokens,omitempty"`
	// Cached marks a verdict served from the verdict cache without calling the engine.
	Cached bool `json:"cached,omitempty"`
}
```

`content_moderation_typesafe.go` 的返回语句改成：

```go
	return &moderationAPIResult{CategoryScores: result.Scores, EngineMeta: &ContentModerationEngineMeta{
		Engine: ContentModerationEngineTypeSafe, Model: result.Model, RulesVersion: TypeSafeModerationRulesVersion, SkippedImages: skipped,
		InputTokens: result.Usage.InputTokens,
	}}, nil
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd backend && go test ./internal/service -run 'TestTypeSafeModerationRecordsInputTokens|TestContentModerationTypeSafe' -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/service/content_moderation_engines.go backend/internal/service/content_moderation_typesafe.go backend/internal/service/content_moderation_typesafe_usage_test.go
git commit -m "feat(moderation): 记录 Jev 审计调用的输入 token"
```

---

### Task 2: 审计日统计（Redis）

**Files:**
- Create: `backend/internal/service/content_moderation_stats.go`
- Create: `backend/internal/repository/content_moderation_stats_store.go`
- Modify: `backend/internal/service/content_moderation.go`（`ContentModerationService` 结构体加字段；`Check` 的关键词/哈希拦截分支；`checkSync`）
- Modify: `backend/internal/service/wire.go`（新增 `ProvideContentModerationService`，替换 ProviderSet 中的 `NewContentModerationService`）
- Modify: `backend/internal/repository/wire.go`（ProviderSet 加 `NewContentModerationStatsStore`）
- Regenerate: `backend/cmd/server/wire_gen.go`
- Test: `backend/internal/service/content_moderation_stats_test.go`（新）、`backend/internal/repository/content_moderation_stats_store_test.go`（新）

**Interfaces:**
- Produces:
  - `type ContentModerationDailyStats struct { Audits, UpstreamCalls, CacheHits, InputTokens, Errors, Flagged, Blocked, ObserveFlagged, HashBlocks, KeywordBlocks int64 }` 及方法 `Plus(other ContentModerationDailyStats) ContentModerationDailyStats`
  - `type ContentModerationStatsStore interface { Add(ctx context.Context, day string, delta ContentModerationDailyStats) error; Get(ctx context.Context, day string) (ContentModerationDailyStats, error) }`
  - `func ContentModerationReportLocation() *time.Location`、`func ContentModerationStatsDay(t time.Time) string`（格式 `20060102`，按 Asia/Shanghai）
  - `func (s *ContentModerationService) SetStatsStore(store ContentModerationStatsStore)`
  - `func ProvideContentModerationService(settingRepo SettingRepository, repo ContentModerationRepository, hashCache ContentModerationHashCache, groupRepo GroupRepository, userRepo UserRepository, proxyRepo ProxyRepository, authCacheInvalidator APIKeyAuthCacheInvalidator, emailService *EmailService, statsStore ContentModerationStatsStore) *ContentModerationService`（Task 7、11 会追加参数）
  - `repository.NewContentModerationStatsStore(rdb *redis.Client) service.ContentModerationStatsStore`
  - 测试 fake：`fakeModerationStatsStore`（`total ContentModerationDailyStats`）

- [ ] **Step 1: 写 Redis 存储的失败测试**

`backend/internal/repository/content_moderation_stats_store_test.go`：

```go
package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestContentModerationStatsStoreAccumulatesPerDay(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	store := NewContentModerationStatsStore(client)
	ctx := context.Background()

	require.NoError(t, store.Add(ctx, "20261115", service.ContentModerationDailyStats{Audits: 1, UpstreamCalls: 1, InputTokens: 900}))
	require.NoError(t, store.Add(ctx, "20261115", service.ContentModerationDailyStats{Audits: 1, CacheHits: 1, Flagged: 1, Blocked: 1}))
	require.NoError(t, store.Add(ctx, "20261116", service.ContentModerationDailyStats{Audits: 5}))

	got, err := store.Get(ctx, "20261115")
	require.NoError(t, err)
	require.Equal(t, service.ContentModerationDailyStats{Audits: 2, UpstreamCalls: 1, CacheHits: 1, InputTokens: 900, Flagged: 1, Blocked: 1}, got)
	require.Greater(t, server.TTL("content_moderation:stats:20261115").Hours(), 24.0*7)

	empty, err := store.Get(ctx, "20200101")
	require.NoError(t, err)
	require.Zero(t, empty)
}
```

- [ ] **Step 2: 写服务埋点的失败测试**

`backend/internal/service/content_moderation_stats_test.go`：

```go
package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeModerationStatsStore struct {
	mu    sync.Mutex
	total ContentModerationDailyStats
	days  []string
}

func (f *fakeModerationStatsStore) Add(_ context.Context, day string, delta ContentModerationDailyStats) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.days = append(f.days, day)
	f.total = f.total.Plus(delta)
	return nil
}

func (f *fakeModerationStatsStore) Get(_ context.Context, _ string) (ContentModerationDailyStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.total, nil
}

func (f *fakeModerationStatsStore) snapshot() ContentModerationDailyStats {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.total
}

// newOpenAIModerationTestService builds a service whose OpenAI-compatible engine always returns the given sexual score.
func newOpenAIModerationTestService(t *testing.T, sexualScore float64, mutate func(*ContentModerationConfig)) (*ContentModerationService, *contentModerationTestRepo, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": sexualScore}}}})
	}))
	t.Cleanup(server.Close)
	cfg := defaultContentModerationConfig()
	cfg.Enabled, cfg.Mode = true, ContentModerationModePreBlock
	cfg.EmailOnHit, cfg.AutoBanEnabled = false, false
	cfg.BaseURL, cfg.APIKeys = server.URL, []string{"sk-test"}
	if mutate != nil {
		mutate(cfg)
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	repo := &contentModerationTestRepo{}
	svc := &ContentModerationService{
		settingRepo: &contentModerationTestSettingRepo{values: map[string]string{
			SettingKeyRiskControlEnabled: "true", SettingKeyContentModerationConfig: string(raw),
		}},
		repo: repo, hashCache: &contentModerationTestHashCache{hashes: map[string]struct{}{}},
		httpClient: server.Client(), asyncQueue: make(chan contentModerationTask, 16), keyHealth: map[string]*contentModerationKeyHealth{},
	}
	return svc, repo, server
}

func moderationChatBody(text string) []byte {
	raw, _ := json.Marshal(map[string]any{"messages": []map[string]any{{"role": "user", "content": text}}})
	return raw
}

func TestContentModerationStatsCountsBlockedAudit(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.99, nil)
	store := &fakeModerationStatsStore{}
	svc.SetStatsStore(store)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 7, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("bad prompt")})

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationDailyStats{Audits: 1, UpstreamCalls: 1, Flagged: 1, Blocked: 1}, store.snapshot())
	require.Equal(t, []string{ContentModerationStatsDay(time.Now())}, store.days)
}

func TestContentModerationStatsCountsKeywordBlock(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.0, func(cfg *ContentModerationConfig) {
		cfg.BlockedKeywords = []string{"forbidden-word"}
	})
	store := &fakeModerationStatsStore{}
	svc.SetStatsStore(store)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 7, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("forbidden-word here")})

	require.NoError(t, err)
	require.True(t, decision.Blocked)
	require.Equal(t, ContentModerationDailyStats{Audits: 1, KeywordBlocks: 1}, store.snapshot())
}

func TestContentModerationStatsDayUsesShanghai(t *testing.T) {
	utc := time.Date(2026, 11, 14, 16, 30, 0, 0, time.UTC) // 2026-11-15 00:30 in Shanghai
	require.Equal(t, "20261115", ContentModerationStatsDay(utc))
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd backend && go test ./internal/service -run TestContentModerationStats -count=1 && go test ./internal/repository -run TestContentModerationStatsStore -count=1`
Expected: 编译失败，`ContentModerationDailyStats` / `SetStatsStore` / `NewContentModerationStatsStore` undefined

- [ ] **Step 4: 实现服务层类型**

`backend/internal/service/content_moderation_stats.go`：

```go
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
```

- [ ] **Step 5: 在审计流程埋点**

`content_moderation.go`：

1. `ContentModerationService` 结构体末尾加字段 `statsStore ContentModerationStatsStore`。
2. `Check` 的关键词拦截分支里，`s.enqueueRecord(input, cfg, log, ...)` 之前加（观察名单用户不会被真正拦截，单独计数）：
   ```go
   keywordDelta := ContentModerationDailyStats{Audits: 1, KeywordBlocks: 1}
   if input.riskControlLogOnly {
   	keywordDelta = ContentModerationDailyStats{Audits: 1, ObserveFlagged: 1}
   }
   s.recordStats(keywordDelta)
   ```
3. `Check` 的哈希拦截分支里，`s.enqueueRecord(input, cfg, log, hashText, false, false)` 之前加：
   ```go
   hashDelta := ContentModerationDailyStats{Audits: 1, HashBlocks: 1}
   if input.riskControlLogOnly {
   	hashDelta = ContentModerationDailyStats{Audits: 1, ObserveFlagged: 1}
   }
   s.recordStats(hashDelta)
   ```
4. `checkSync` 里，`callModeration` 返回 `err != nil` 的分支开头加：
   ```go
   s.recordStats(ContentModerationDailyStats{Audits: 1, UpstreamCalls: 1, Errors: 1})
   ```
5. `checkSync` 里，计算出 `action`/`blocked` 之后（`if trackPreBlock { s.recordPreBlockSyncMetric(latency, action) }` 之前）加：
   ```go
   delta := ContentModerationDailyStats{Audits: 1, UpstreamCalls: 1}
   if result.EngineMeta != nil {
   	delta.InputTokens = int64(result.EngineMeta.InputTokens)
   }
   if flagged {
   	delta.Flagged = 1
   	if input.riskControlLogOnly {
   		delta.ObserveFlagged = 1
   	}
   }
   if blocked {
   	delta.Blocked = 1
   }
   s.recordStats(delta)
   ```
   （Task 11 会把命中缓存时的 `UpstreamCalls` 改成 `CacheHits`。）

- [ ] **Step 6: 实现 Redis 存储**

`backend/internal/repository/content_moderation_stats_store.go`：

```go
package repository

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	contentModerationStatsKeyPrefix = "content_moderation:stats:"
	contentModerationStatsTTL       = 8 * 24 * time.Hour
)

type contentModerationStatsStore struct {
	rdb *redis.Client
}

func NewContentModerationStatsStore(rdb *redis.Client) service.ContentModerationStatsStore {
	return &contentModerationStatsStore{rdb: rdb}
}

func contentModerationStatsFields(stats *service.ContentModerationDailyStats) map[string]*int64 {
	return map[string]*int64{
		"audits": &stats.Audits, "upstream_calls": &stats.UpstreamCalls, "cache_hits": &stats.CacheHits,
		"input_tokens": &stats.InputTokens, "errors": &stats.Errors, "flagged": &stats.Flagged,
		"blocked": &stats.Blocked, "observe_flagged": &stats.ObserveFlagged,
		"hash_blocks": &stats.HashBlocks, "keyword_blocks": &stats.KeywordBlocks,
	}
}

func (s *contentModerationStatsStore) Add(ctx context.Context, day string, delta service.ContentModerationDailyStats) error {
	if s == nil || s.rdb == nil {
		return nil
	}
	key := contentModerationStatsKeyPrefix + day
	pipe := s.rdb.TxPipeline()
	for field, value := range contentModerationStatsFields(&delta) {
		if *value != 0 {
			pipe.HIncrBy(ctx, key, field, *value)
		}
	}
	pipe.Expire(ctx, key, contentModerationStatsTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *contentModerationStatsStore) Get(ctx context.Context, day string) (service.ContentModerationDailyStats, error) {
	var out service.ContentModerationDailyStats
	if s == nil || s.rdb == nil {
		return out, nil
	}
	values, err := s.rdb.HGetAll(ctx, contentModerationStatsKeyPrefix+day).Result()
	if err != nil {
		return out, err
	}
	for field, target := range contentModerationStatsFields(&out) {
		if raw, ok := values[field]; ok {
			if n, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil {
				*target = n
			}
		}
	}
	return out, nil
}
```

- [ ] **Step 7: 接线**

`backend/internal/service/wire.go` 新增（放在 `ProvideOpsAlertEvaluatorService` 附近）：

```go
// ProvideContentModerationService wires optional collaborators via setters so existing
// NewContentModerationService call sites (mostly tests) stay unchanged.
func ProvideContentModerationService(
	settingRepo SettingRepository,
	repo ContentModerationRepository,
	hashCache ContentModerationHashCache,
	groupRepo GroupRepository,
	userRepo UserRepository,
	proxyRepo ProxyRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	emailService *EmailService,
	statsStore ContentModerationStatsStore,
) *ContentModerationService {
	svc := NewContentModerationService(settingRepo, repo, hashCache, groupRepo, userRepo, proxyRepo, authCacheInvalidator, emailService)
	svc.SetStatsStore(statsStore)
	return svc
}
```

ProviderSet 中把 `NewContentModerationService,` 替换为 `ProvideContentModerationService,`。`backend/internal/repository/wire.go` 的 ProviderSet 在 `NewContentModerationHashCache,` 下一行加 `NewContentModerationStatsStore,`。

Run: `cd backend && go generate ./cmd/server`
Expected: `wire_gen.go` 中出现 `repository.NewContentModerationStatsStore(redisClient)` 和 `service.ProvideContentModerationService(...)`

- [ ] **Step 8: 运行测试确认通过**

Run: `cd backend && go test ./internal/service -run 'TestContentModeration' -count=1 && go test ./internal/repository -run TestContentModerationStatsStore -count=1 && go build ./...`
Expected: PASS，编译通过

- [ ] **Step 9: 提交**

```bash
git add backend/internal/service/content_moderation_stats.go backend/internal/service/content_moderation_stats_test.go backend/internal/service/content_moderation.go backend/internal/service/wire.go backend/internal/repository/content_moderation_stats_store.go backend/internal/repository/content_moderation_stats_store_test.go backend/internal/repository/wire.go backend/cmd/server/wire_gen.go
git commit -m "feat(moderation): 增加审计日统计（Redis 计数）"
```

---

### Task 3: 402 单独冻结，日志写真实错误

**Files:**
- Modify: `backend/internal/service/content_moderation.go`（常量区约 :84；`callModeration` :1733-1782；`contentModerationFreezeDurationForHTTPStatus` :2435）
- Test: `backend/internal/service/content_moderation_balance_test.go`（新）

**Interfaces:**
- Produces: 常量 `contentModerationKeyBalanceFreezeDuration = 10 * time.Minute`；方法 `func (s *ContentModerationService) noUsableAPIKeyError(cfg *ContentModerationConfig) error`。
- Consumes: Task 1 的 `newTypeSafeTestConfig`。

- [ ] **Step 1: 写失败的测试**

```go
package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestContentModerationFreezesKeyLongOnPaymentRequired(t *testing.T) {
	require.Equal(t, 10*time.Minute, contentModerationFreezeDurationForHTTPStatus(http.StatusPaymentRequired))
}

func TestContentModerationPaymentRequiredKeepsRealError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer server.Close()
	svc := &ContentModerationService{httpClient: server.Client(), keyHealth: map[string]*contentModerationKeyHealth{}}
	cfg := newTypeSafeTestConfig(server.URL)

	_, err := svc.callModeration(context.Background(), cfg, "需要审核的文字")
	require.EqualError(t, err, "typesafe API status 402")
	require.Equal(t, int32(1), calls.Load())

	_, err = svc.callModeration(context.Background(), cfg, "需要审核的文字")
	require.EqualError(t, err, "no moderation api key available: typesafe API status 402")
	require.Equal(t, int32(1), calls.Load(), "frozen key must not be called again")
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test ./internal/service -run 'TestContentModerationFreezesKeyLongOnPaymentRequired|TestContentModerationPaymentRequiredKeepsRealError' -count=1`
Expected: FAIL（冻结时长是 10s；第一次的错误是 `no moderation api key available`）

- [ ] **Step 3: 实现**

常量区 `contentModerationKeyHTTPErrorFreezeDuration` 下一行加：

```go
	contentModerationKeyBalanceFreezeDuration    = 10 * time.Minute
```

`contentModerationFreezeDurationForHTTPStatus` 的 switch 里加一支：

```go
	case http.StatusPaymentRequired:
		return contentModerationKeyBalanceFreezeDuration
```

`callModeration` 里把

```go
		key, ok := s.nextUsableAPIKey(cfg)
		if !ok {
			lastErr = errors.New("no moderation api key available")
			break
		}
```

改成

```go
		key, ok := s.nextUsableAPIKey(cfg)
		if !ok {
			if lastErr == nil {
				lastErr = s.noUsableAPIKeyError(cfg)
			}
			break
		}
```

并在文件中 `nextUsableAPIKey` 后新增：

```go
// noUsableAPIKeyError surfaces the frozen key's last real error (e.g. a 402) instead of a generic message.
func (s *ContentModerationService) noUsableAPIKeyError(cfg *ContentModerationConfig) error {
	if s != nil {
		s.keyHealthMu.Lock()
		defer s.keyHealthMu.Unlock()
		for _, key := range cfg.apiKeys() {
			if state := s.keyHealth[scopedModerationKeyHash(key, cfg.Engine)]; state != nil && state.LastError != "" {
				return fmt.Errorf("no moderation api key available: %s", state.LastError)
			}
		}
	}
	return errors.New("no moderation api key available")
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd backend && go test ./internal/service -run 'TestContentModeration' -count=1`
Expected: PASS（若已有测试断言完整错误串 `no moderation api key available`，改成 `require.ErrorContains`）

- [ ] **Step 5: 提交**

```bash
git add backend/internal/service/content_moderation.go backend/internal/service/content_moderation_balance_test.go
git commit -m "fix(moderation): 402 余额耗尽冻结 10 分钟并保留真实错误"
```

---

### Task 4: 审计健康监控与系统告警事件

**Files:**
- Create: `backend/internal/service/content_moderation_notify.go`
- Create: `backend/internal/service/content_moderation_health.go`
- Modify: `backend/internal/service/content_moderation.go`（结构体字段；`callModeration` 每次尝试后上报）
- Test: `backend/internal/service/content_moderation_health_test.go`（新）

**Interfaces:**
- Produces:
  - `type ModerationAlertKind string`，常量 `ModerationAlertBalanceExhausted = "balance_exhausted"`、`ModerationAlertAllKeysFrozen = "all_keys_frozen"`、`ModerationAlertFailureRate = "failure_rate"`
  - `type ModerationSystemAlert struct { Kind ModerationAlertKind; Resolved bool; Detail string; At time.Time }`
  - `type ModerationHitEvent struct { UserID int64; UserEmail, GroupName, Model, Category string; Score float64; Action string; Observe bool; InputHash, RequestID string; At time.Time }`
  - `type ContentModerationNotifier interface { NotifyModerationHit(ctx context.Context, ev ModerationHitEvent); NotifySystemAlert(ctx context.Context, alert ModerationSystemAlert) }`
  - `func (s *ContentModerationService) SetNotifier(n ContentModerationNotifier)`
  - 测试 fake：`fakeModerationNotifier`（字段 `hits []ModerationHitEvent`、`alerts []ModerationSystemAlert`，方法 `snapshotAlerts()`、`snapshotHits()`）

- [ ] **Step 1: 写失败的测试**

```go
package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeModerationNotifier struct {
	mu     sync.Mutex
	hits   []ModerationHitEvent
	alerts []ModerationSystemAlert
}

func (f *fakeModerationNotifier) NotifyModerationHit(_ context.Context, ev ModerationHitEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hits = append(f.hits, ev)
}

func (f *fakeModerationNotifier) NotifySystemAlert(_ context.Context, alert ModerationSystemAlert) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, alert)
}

func (f *fakeModerationNotifier) snapshotAlerts() []ModerationSystemAlert {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ModerationSystemAlert(nil), f.alerts...)
}

func (f *fakeModerationNotifier) snapshotHits() []ModerationHitEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ModerationHitEvent(nil), f.hits...)
}

func TestModerationHealthBalanceAlertRepeatsEvery30MinutesAndResolves(t *testing.T) {
	var h contentModerationHealth
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

	alerts := h.observe(t0, moderationAttemptOutcome{HTTPStatus: http.StatusPaymentRequired})
	require.Len(t, alerts, 1)
	require.Equal(t, ModerationAlertBalanceExhausted, alerts[0].Kind)
	require.False(t, alerts[0].Resolved)

	require.Empty(t, h.observe(t0.Add(10*time.Minute), moderationAttemptOutcome{HTTPStatus: http.StatusPaymentRequired}))
	require.Empty(t, h.observe(t0.Add(11*time.Minute), moderationAttemptOutcome{NoKeyAvailable: true, Err: "frozen"}), "frozen alert is suppressed while balance alert fires")

	again := h.observe(t0.Add(31*time.Minute), moderationAttemptOutcome{HTTPStatus: http.StatusPaymentRequired})
	require.Len(t, again, 1)

	resolved := h.observe(t0.Add(32*time.Minute), moderationAttemptOutcome{Success: true})
	require.Equal(t, []ModerationSystemAlert{{Kind: ModerationAlertBalanceExhausted, Resolved: true, At: t0.Add(32 * time.Minute)}}, resolved)
}

func TestModerationHealthFailureRateNeedsMinimumCalls(t *testing.T) {
	var h contentModerationHealth
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	var fired []ModerationSystemAlert
	for i := 0; i < 19; i++ {
		fired = append(fired, h.observe(t0, moderationAttemptOutcome{HTTPStatus: http.StatusBadGateway})...)
	}
	require.Empty(t, fired, "below 20 calls no rate alert")

	fired = h.observe(t0.Add(time.Minute), moderationAttemptOutcome{HTTPStatus: http.StatusBadGateway})
	require.Len(t, fired, 1)
	require.Equal(t, ModerationAlertFailureRate, fired[0].Kind)
	require.Contains(t, fired[0].Detail, "20/20")

	var resolved []ModerationSystemAlert
	for i := 0; i < 80; i++ {
		resolved = append(resolved, h.observe(t0.Add(2*time.Minute), moderationAttemptOutcome{Success: true})...)
	}
	require.Len(t, resolved, 1)
	require.True(t, resolved[0].Resolved)
}

func TestModerationHealthIgnoresBucketsOlderThanFiveMinutes(t *testing.T) {
	var h contentModerationHealth
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 30; i++ {
		h.observe(t0, moderationAttemptOutcome{HTTPStatus: http.StatusBadGateway})
	}
	calls, failures := h.windowTotals(t0.Add(6*time.Minute).Unix() / 60)
	require.Zero(t, calls)
	require.Zero(t, failures)
}

func TestContentModerationNotifiesBalanceExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer server.Close()
	notifier := &fakeModerationNotifier{}
	svc := &ContentModerationService{httpClient: server.Client(), keyHealth: map[string]*contentModerationKeyHealth{}}
	svc.SetNotifier(notifier)

	_, err := svc.callModeration(context.Background(), newTypeSafeTestConfig(server.URL), "需要审核的文字")

	require.Error(t, err)
	alerts := notifier.snapshotAlerts()
	require.Len(t, alerts, 1)
	require.Equal(t, ModerationAlertBalanceExhausted, alerts[0].Kind)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test ./internal/service -run 'TestModerationHealth|TestContentModerationNotifiesBalanceExhausted' -count=1`
Expected: 编译失败，`contentModerationHealth` undefined

- [ ] **Step 3: 实现通知接口**

`backend/internal/service/content_moderation_notify.go`：

```go
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
```

- [ ] **Step 4: 实现健康状态机**

`backend/internal/service/content_moderation_health.go`：

```go
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
```

- [ ] **Step 5: 接入 `callModeration`**

`content_moderation.go`：结构体加字段 `health contentModerationHealth` 和 `notifier ContentModerationNotifier`。`callModeration` 中：

- 无可用 key 的分支（Task 3 改过的那段）在 `break` 之前加：
  ```go
  			s.reportAttempt(moderationAttemptOutcome{NoKeyAvailable: true, Err: lastErr.Error()})
  ```
- `result, err := s.callModerationOnceWithInput(...)` 之后、`if err == nil {` 之前加：
  ```go
  		outcome := moderationAttemptOutcome{Success: err == nil, HTTPStatus: httpStatus}
  		if err != nil {
  			outcome.Err = err.Error()
  		}
  		s.reportAttempt(outcome)
  ```

- [ ] **Step 6: 运行测试确认通过**

Run: `cd backend && go test ./internal/service -run 'TestModerationHealth|TestContentModeration' -count=1`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add backend/internal/service/content_moderation_notify.go backend/internal/service/content_moderation_health.go backend/internal/service/content_moderation_health_test.go backend/internal/service/content_moderation.go
git commit -m "feat(moderation): 审计余额耗尽/key 冻结/失败率告警事件"
```

---

### Task 5: Server酱 Turbo 客户端

**Files:**
- Create: `backend/internal/pkg/serverchan/client.go`
- Test: `backend/internal/pkg/serverchan/client_test.go`

**Interfaces:**
- Produces:
  - `func ValidSendKey(key string) bool`
  - `func TruncateTitle(title string) string`（去换行，最多 32 个 rune）
  - `type Client struct { HTTP *http.Client; EndpointFor func(sendKey string) string }`
  - `func NewClient(httpClient *http.Client) *Client`
  - `func (c *Client) Send(ctx context.Context, sendKey, title, desp string) error`
  - `var ErrInvalidSendKey = errors.New("serverchan: invalid send key")`

- [ ] **Step 1: 写失败的测试**

```go
package serverchan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidSendKey(t *testing.T) {
	require.True(t, ValidSendKey("SCT470902TabcDEF123"))
	require.False(t, ValidSendKey(""))
	require.False(t, ValidSendKey("sctp123t456"))
	require.False(t, ValidSendKey("SCT123/../x"))
}

func TestTruncateTitle(t *testing.T) {
	require.Equal(t, "a b", TruncateTitle("a\nb"))
	long := strings.Repeat("审", 40)
	require.Equal(t, strings.Repeat("审", 32), TruncateTitle(long))
}

func TestSendPostsFormAndChecksCode(t *testing.T) {
	var gotPath, gotTitle, gotDesp string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		require.NoError(t, r.ParseForm())
		gotTitle, gotDesp = r.PostForm.Get("title"), r.PostForm.Get("desp")
		_, _ = w.Write([]byte(`{"code":0,"message":"","data":{"pushid":"1"}}`))
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), EndpointFor: func(key string) string { return server.URL + "/" + key + ".send" }}

	require.NoError(t, client.Send(context.Background(), "SCTkey123", "标题\n换行", "**正文**"))
	require.Equal(t, "/SCTkey123.send", gotPath)
	require.Equal(t, "标题 换行", gotTitle)
	require.Equal(t, "**正文**", gotDesp)
}

func TestSendErrorsNeverContainKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":40001,"message":"bad key SCTkey123"}`))
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), EndpointFor: func(key string) string { return server.URL + "/" + key + ".send" }}

	err := client.Send(context.Background(), "SCTkey123", "t", "d")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SCTkey123")
	require.Contains(t, err.Error(), "40001")

	require.ErrorIs(t, client.Send(context.Background(), "bad", "t", "d"), ErrInvalidSendKey)
}

func TestSendHTTPStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := &Client{HTTP: server.Client(), EndpointFor: func(key string) string { return server.URL + "/" + key + ".send" }}
	require.ErrorContains(t, client.Send(context.Background(), "SCTkey123", "t", "d"), "429")
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test ./internal/pkg/serverchan -count=1`
Expected: 编译失败（包不存在）

- [ ] **Step 3: 实现**

```go
// Package serverchan sends WeChat notifications through ServerChan Turbo (sct.ftqq.com).
package serverchan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const maxTitleRunes = 32

var (
	ErrInvalidSendKey = errors.New("serverchan: invalid send key")
	sendKeyPattern    = regexp.MustCompile(`^SCT[0-9A-Za-z]+$`)
)

func ValidSendKey(key string) bool { return sendKeyPattern.MatchString(key) }

// TruncateTitle removes line breaks (ServerChan rejects them) and keeps at most 32 runes.
func TruncateTitle(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	runes := []rune(title)
	if len(runes) > maxTitleRunes {
		runes = runes[:maxTitleRunes]
	}
	return string(runes)
}

type Client struct {
	HTTP        *http.Client
	EndpointFor func(sendKey string) string
}

func NewClient(httpClient *http.Client) *Client {
	return &Client{HTTP: httpClient, EndpointFor: func(key string) string {
		return "https://sctapi.ftqq.com/" + key + ".send"
	}}
}

func (c *Client) Send(ctx context.Context, sendKey, title, desp string) error {
	if !ValidSendKey(sendKey) {
		return ErrInvalidSendKey
	}
	form := url.Values{"title": {TruncateTitle(title)}, "desp": {desp}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.EndpointFor(sendKey), strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("serverchan: build request failed")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// Transport errors can embed the URL (which contains the key); never return them verbatim.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("serverchan: transport error")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("serverchan: http status %d", resp.StatusCode)
	}
	var out struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out); err != nil {
		return errors.New("serverchan: invalid response")
	}
	if out.Code != 0 {
		return fmt.Errorf("serverchan: api code %d", out.Code)
	}
	return nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd backend && go test ./internal/pkg/serverchan -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/pkg/serverchan
git commit -m "feat(push): 新增 Server酱 Turbo 客户端"
```

---

### Task 6: 推送配置与后台接口

**Files:**
- Modify: `backend/internal/service/domain_constants.go`（`SettingKeyContentModerationConfig` 附近加常量）
- Create: `backend/internal/service/push_notification_config.go`
- Create: `backend/internal/service/push_notification_service.go`（本任务只放结构体、构造函数、`loadConfig`；Task 7 补队列）
- Create: `backend/internal/repository/push_notification_store.go`
- Create: `backend/internal/handler/admin/push_notification_handler.go`
- Modify: `backend/internal/handler/handler.go`（`AdminHandlers` 加 `PushNotification *admin.PushNotificationHandler`）
- Modify: `backend/internal/handler/wire.go`（`ProvideAdminHandlers` 加参数并赋值；ProviderSet 加 `admin.NewPushNotificationHandler`）
- Modify: `backend/internal/server/routes/admin.go:172-182`（`registerContentModerationRoutes` 加 3 条路由）
- Modify: `backend/internal/service/wire.go`（新增 `ProvidePushNotificationService`）、`backend/internal/repository/wire.go`（加 `NewPushNotificationStore`）
- Regenerate: `backend/cmd/server/wire_gen.go`
- Test: `backend/internal/service/push_notification_config_test.go`、`backend/internal/repository/push_notification_store_test.go`、`backend/internal/handler/admin/push_notification_handler_test.go`

**Interfaces:**
- Produces:
  - `const SettingKeyPushNotificationConfig = "push_notification_config"`
  - `type PushNotificationConfig struct { Enabled bool; SendKey string; HitDailyCap, HitWindowMinutes int; DailyReportEnabled bool; DailyReportHour int; OpsCriticalEnabled bool }`（json：`enabled`、`send_key`、`hit_daily_cap`、`hit_window_minutes`、`daily_report_enabled`、`daily_report_hour`、`ops_critical_enabled`）
  - `type PushNotificationConfigView struct`（同上字段，但用 `SendKeyConfigured bool`、`SendKeyMasked string` 替代 `SendKey`）
  - `type UpdatePushNotificationConfigInput struct { Enabled *bool; SendKey *string; ClearSendKey bool; HitDailyCap, HitWindowMinutes *int; DailyReportEnabled *bool; DailyReportHour *int; OpsCriticalEnabled *bool }`
  - `type PushSender interface { Send(ctx context.Context, sendKey, title, desp string) error }`
  - `type PushNotificationStore interface { IncrSent(ctx context.Context, day, category string) (int64, error); GetSent(ctx context.Context, day string) (map[string]int64, error); TryOnce(ctx context.Context, key string, ttl time.Duration) (bool, error) }`
  - `type PushNotificationService struct`；`func NewPushNotificationService(settingRepo SettingRepository, store PushNotificationStore, sender PushSender) *PushNotificationService`
  - 方法：`GetConfig(ctx) (*PushNotificationConfigView, error)`、`UpdateConfig(ctx, UpdatePushNotificationConfigInput) (*PushNotificationConfigView, error)`、`SendTest(ctx) error`、`loadConfig(ctx) *PushNotificationConfig`
  - `repository.NewPushNotificationStore(rdb *redis.Client) service.PushNotificationStore`
  - 路由：`GET /api/v1/admin/risk-control/push/config`、`PUT /api/v1/admin/risk-control/push/config`、`POST /api/v1/admin/risk-control/push/test`
  - 测试 fake：`fakePushSender`（`sent []fakePush{Key, Title, Desp string}`，`failTimes int`）、`fakePushStore`

- [ ] **Step 1: 写服务层失败测试**

`backend/internal/service/push_notification_config_test.go`：

```go
package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakePush struct{ Key, Title, Desp string }

type fakePushSender struct {
	mu        sync.Mutex
	sent      []fakePush
	failTimes int
}

func (f *fakePushSender) Send(_ context.Context, key, title, desp string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failTimes > 0 {
		f.failTimes--
		return errors.New("send failed")
	}
	f.sent = append(f.sent, fakePush{Key: key, Title: title, Desp: desp})
	return nil
}

func (f *fakePushSender) snapshot() []fakePush {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakePush(nil), f.sent...)
}

type fakePushStore struct {
	mu   sync.Mutex
	sent map[string]map[string]int64
	once map[string]bool
}

func (f *fakePushStore) IncrSent(_ context.Context, day, category string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sent == nil {
		f.sent = map[string]map[string]int64{}
	}
	if f.sent[day] == nil {
		f.sent[day] = map[string]int64{}
	}
	f.sent[day][category]++
	return f.sent[day][category], nil
}

func (f *fakePushStore) GetSent(_ context.Context, day string) (map[string]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int64{}
	for k, v := range f.sent[day] {
		out[k] = v
	}
	return out, nil
}

func (f *fakePushStore) TryOnce(_ context.Context, key string, _ time.Duration) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.once == nil {
		f.once = map[string]bool{}
	}
	if f.once[key] {
		return false, nil
	}
	f.once[key] = true
	return true, nil
}

func TestPushNotificationConfigMasksKeyAndValidates(t *testing.T) {
	repo := &contentModerationTestSettingRepo{values: map[string]string{}}
	svc := NewPushNotificationService(repo, &fakePushStore{}, &fakePushSender{})
	ctx := context.Background()

	view, err := svc.GetConfig(ctx)
	require.NoError(t, err)
	require.False(t, view.Enabled)
	require.Equal(t, 300, view.HitDailyCap)
	require.Equal(t, 30, view.HitWindowMinutes)
	require.Equal(t, 9, view.DailyReportHour)
	require.True(t, view.DailyReportEnabled)
	require.True(t, view.OpsCriticalEnabled)

	enabled := true
	_, err = svc.UpdateConfig(ctx, UpdatePushNotificationConfigInput{Enabled: &enabled})
	require.ErrorContains(t, err, "SendKey")

	bad := "not-a-key"
	_, err = svc.UpdateConfig(ctx, UpdatePushNotificationConfigInput{SendKey: &bad})
	require.ErrorContains(t, err, "SCT")

	key := "SCT470902TabcDEF123"
	cap := 5000
	view, err = svc.UpdateConfig(ctx, UpdatePushNotificationConfigInput{Enabled: &enabled, SendKey: &key, HitDailyCap: &cap})
	require.NoError(t, err)
	require.True(t, view.SendKeyConfigured)
	require.Equal(t, "********F123", view.SendKeyMasked)
	require.Equal(t, 1000, view.HitDailyCap)
	raw, _ := json.Marshal(view)
	require.NotContains(t, string(raw), key)

	// omitting send_key keeps the stored key
	hour := 8
	view, err = svc.UpdateConfig(ctx, UpdatePushNotificationConfigInput{DailyReportHour: &hour})
	require.NoError(t, err)
	require.True(t, view.SendKeyConfigured)
	require.Equal(t, 8, view.DailyReportHour)
	require.Equal(t, key, svc.loadConfig(ctx).SendKey)

	// clearing the key while enabled is rejected
	_, err = svc.UpdateConfig(ctx, UpdatePushNotificationConfigInput{ClearSendKey: true})
	require.ErrorContains(t, err, "SendKey")
}

func TestPushNotificationSendTestBypassesQueue(t *testing.T) {
	key := "SCT470902TabcDEF123"
	raw, _ := json.Marshal(PushNotificationConfig{Enabled: false, SendKey: key})
	sender := &fakePushSender{}
	svc := NewPushNotificationService(&contentModerationTestSettingRepo{values: map[string]string{SettingKeyPushNotificationConfig: string(raw)}}, &fakePushStore{}, sender)

	require.NoError(t, svc.SendTest(context.Background()))
	sent := sender.snapshot()
	require.Len(t, sent, 1)
	require.Equal(t, key, sent[0].Key)
	require.Contains(t, sent[0].Title, "测试")
}
```

- [ ] **Step 2: 写 Redis 存储失败测试**

`backend/internal/repository/push_notification_store_test.go`：

```go
package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestPushNotificationStoreCountsAndOnce(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	store := NewPushNotificationStore(client)
	ctx := context.Background()

	n, err := store.IncrSent(ctx, "20261115", "hit")
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	n, _ = store.IncrSent(ctx, "20261115", "hit")
	require.Equal(t, int64(2), n)
	_, _ = store.IncrSent(ctx, "20261115", "alert")

	sent, err := store.GetSent(ctx, "20261115")
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"hit": 2, "alert": 1}, sent)

	ok, err := store.TryOnce(ctx, "push_notification:daily_report:20261115", time.Hour)
	require.NoError(t, err)
	require.True(t, ok)
	ok, _ = store.TryOnce(ctx, "push_notification:daily_report:20261115", time.Hour)
	require.False(t, ok)
}
```

- [ ] **Step 3: 写 handler 失败测试**

`backend/internal/handler/admin/push_notification_handler_test.go`（参照同目录 `content_moderation_engine_test.go` 的 gin 测试写法）：

```go
package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type memorySettingRepo struct{ values map[string]string }

func (r *memorySettingRepo) Get(ctx context.Context, key string) (*service.Setting, error) {
	if v, ok := r.values[key]; ok {
		return &service.Setting{Key: key, Value: v}, nil
	}
	return nil, service.ErrSettingNotFound
}
func (r *memorySettingRepo) GetValue(ctx context.Context, key string) (string, error) {
	if v, ok := r.values[key]; ok {
		return v, nil
	}
	return "", service.ErrSettingNotFound
}
func (r *memorySettingRepo) Set(ctx context.Context, key, value string) error { r.values[key] = value; return nil }
func (r *memorySettingRepo) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := r.values[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}
func (r *memorySettingRepo) SetMultiple(ctx context.Context, settings map[string]string) error {
	for k, v := range settings {
		r.values[k] = v
	}
	return nil
}
func (r *memorySettingRepo) GetAll(ctx context.Context) (map[string]string, error) { return r.values, nil }
func (r *memorySettingRepo) Delete(ctx context.Context, key string) error         { delete(r.values, key); return nil }

func TestPushNotificationHandlerUpdateHidesKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewPushNotificationService(&memorySettingRepo{values: map[string]string{}}, nil, nil)
	h := NewPushNotificationHandler(svc)
	router := gin.New()
	router.PUT("/push/config", h.UpdateConfig)

	body := `{"enabled":true,"send_key":"SCT470902TabcDEF123","hit_daily_cap":200}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/push/config", strings.NewReader(body)))

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"send_key_configured":true`)
	require.Contains(t, w.Body.String(), `"hit_daily_cap":200`)
	require.NotContains(t, w.Body.String(), "SCT470902TabcDEF123")
}
```

> 如果 `SettingRepository` 接口的方法集和上面的 `memorySettingRepo` 不一致，以 `backend/internal/service/content_moderation_test.go:20-77` 的 `contentModerationTestSettingRepo` 为准补齐方法。

- [ ] **Step 4: 运行测试确认失败**

Run: `cd backend && go test ./internal/service -run TestPushNotification -count=1; go test ./internal/repository -run TestPushNotificationStore -count=1; go test ./internal/handler/admin -run TestPushNotificationHandler -count=1`
Expected: 编译失败（类型不存在）

- [ ] **Step 5: 实现配置**

`domain_constants.go` 在 `SettingKeyContentModerationConfig` 下一行加：

```go
	SettingKeyPushNotificationConfig              = "push_notification_config"         // Server酱微信推送配置（JSON，含 SendKey）
```

`backend/internal/service/push_notification_config.go`：

```go
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/serverchan"
)

const (
	defaultPushHitDailyCap      = 300
	maxPushHitDailyCap          = 1000
	defaultPushHitWindowMinutes = 30
	minPushHitWindowMinutes     = 5
	maxPushHitWindowMinutes     = 240
	defaultPushDailyReportHour  = 9
)

type PushNotificationConfig struct {
	Enabled            bool   `json:"enabled"`
	SendKey            string `json:"send_key,omitempty"`
	HitDailyCap        int    `json:"hit_daily_cap"`
	HitWindowMinutes   int    `json:"hit_window_minutes"`
	DailyReportEnabled bool   `json:"daily_report_enabled"`
	DailyReportHour    int    `json:"daily_report_hour"`
	OpsCriticalEnabled bool   `json:"ops_critical_enabled"`
}

type PushNotificationConfigView struct {
	Enabled            bool   `json:"enabled"`
	SendKeyConfigured  bool   `json:"send_key_configured"`
	SendKeyMasked      string `json:"send_key_masked"`
	HitDailyCap        int    `json:"hit_daily_cap"`
	HitWindowMinutes   int    `json:"hit_window_minutes"`
	DailyReportEnabled bool   `json:"daily_report_enabled"`
	DailyReportHour    int    `json:"daily_report_hour"`
	OpsCriticalEnabled bool   `json:"ops_critical_enabled"`
}

type UpdatePushNotificationConfigInput struct {
	Enabled            *bool   `json:"enabled"`
	SendKey            *string `json:"send_key"`
	ClearSendKey       bool    `json:"clear_send_key"`
	HitDailyCap        *int    `json:"hit_daily_cap"`
	HitWindowMinutes   *int    `json:"hit_window_minutes"`
	DailyReportEnabled *bool   `json:"daily_report_enabled"`
	DailyReportHour    *int    `json:"daily_report_hour"`
	OpsCriticalEnabled *bool   `json:"ops_critical_enabled"`
}

func defaultPushNotificationConfig() *PushNotificationConfig {
	return &PushNotificationConfig{
		HitDailyCap: defaultPushHitDailyCap, HitWindowMinutes: defaultPushHitWindowMinutes,
		DailyReportEnabled: true, DailyReportHour: defaultPushDailyReportHour, OpsCriticalEnabled: true,
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (c *PushNotificationConfig) normalize() {
	c.SendKey = strings.TrimSpace(c.SendKey)
	if c.HitDailyCap <= 0 {
		c.HitDailyCap = defaultPushHitDailyCap
	}
	c.HitDailyCap = clampInt(c.HitDailyCap, 1, maxPushHitDailyCap)
	if c.HitWindowMinutes <= 0 {
		c.HitWindowMinutes = defaultPushHitWindowMinutes
	}
	c.HitWindowMinutes = clampInt(c.HitWindowMinutes, minPushHitWindowMinutes, maxPushHitWindowMinutes)
	c.DailyReportHour = clampInt(c.DailyReportHour, 0, 23)
}

func parsePushNotificationConfig(raw string) (*PushNotificationConfig, error) {
	cfg := defaultPushNotificationConfig()
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), cfg); err != nil {
			return nil, fmt.Errorf("parse push notification config: %w", err)
		}
	}
	cfg.normalize()
	return cfg, nil
}

func (c *PushNotificationConfig) view() *PushNotificationConfigView {
	v := &PushNotificationConfigView{
		Enabled: c.Enabled, SendKeyConfigured: c.SendKey != "",
		HitDailyCap: c.HitDailyCap, HitWindowMinutes: c.HitWindowMinutes,
		DailyReportEnabled: c.DailyReportEnabled, DailyReportHour: c.DailyReportHour, OpsCriticalEnabled: c.OpsCriticalEnabled,
	}
	if c.SendKey != "" {
		v.SendKeyMasked = maskSecretTail(c.SendKey)
	}
	return v
}

func (s *PushNotificationService) readConfig(ctx context.Context) (*PushNotificationConfig, error) {
	if s == nil || s.settingRepo == nil {
		return defaultPushNotificationConfig(), nil
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyPushNotificationConfig)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return nil, fmt.Errorf("get push notification config: %w", err)
	}
	return parsePushNotificationConfig(raw)
}

func (s *PushNotificationService) GetConfig(ctx context.Context) (*PushNotificationConfigView, error) {
	cfg, err := s.readConfig(ctx)
	if err != nil {
		return nil, err
	}
	return cfg.view(), nil
}

func (s *PushNotificationService) UpdateConfig(ctx context.Context, in UpdatePushNotificationConfigInput) (*PushNotificationConfigView, error) {
	cfg, err := s.readConfig(ctx)
	if err != nil {
		return nil, err
	}
	if in.ClearSendKey {
		cfg.SendKey = ""
	} else if in.SendKey != nil && strings.TrimSpace(*in.SendKey) != "" {
		key := strings.TrimSpace(*in.SendKey)
		if !serverchan.ValidSendKey(key) {
			return nil, infraerrors.BadRequest("INVALID_PUSH_SEND_KEY", "Server酱 SendKey 格式不正确（应以 SCT 开头）")
		}
		cfg.SendKey = key
	}
	if in.Enabled != nil {
		cfg.Enabled = *in.Enabled
	}
	if in.HitDailyCap != nil {
		cfg.HitDailyCap = *in.HitDailyCap
	}
	if in.HitWindowMinutes != nil {
		cfg.HitWindowMinutes = *in.HitWindowMinutes
	}
	if in.DailyReportEnabled != nil {
		cfg.DailyReportEnabled = *in.DailyReportEnabled
	}
	if in.DailyReportHour != nil {
		cfg.DailyReportHour = *in.DailyReportHour
	}
	if in.OpsCriticalEnabled != nil {
		cfg.OpsCriticalEnabled = *in.OpsCriticalEnabled
	}
	cfg.normalize()
	if cfg.Enabled && cfg.SendKey == "" {
		return nil, infraerrors.BadRequest("PUSH_SEND_KEY_REQUIRED", "启用推送前需要填写 Server酱 SendKey")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal push notification config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyPushNotificationConfig, string(raw)); err != nil {
		return nil, fmt.Errorf("save push notification config: %w", err)
	}
	return cfg.view(), nil
}

// SendTest sends synchronously, bypassing the queue, daily caps and the enabled switch.
func (s *PushNotificationService) SendTest(ctx context.Context) error {
	cfg, err := s.readConfig(ctx)
	if err != nil {
		return err
	}
	if cfg.SendKey == "" {
		return infraerrors.BadRequest("PUSH_SEND_KEY_REQUIRED", "请先填写并保存 Server酱 SendKey")
	}
	if s.sender == nil {
		return errors.New("push sender unavailable")
	}
	if err := s.sender.Send(ctx, cfg.SendKey, "【测试】sub2api 推送测试", "收到这条消息说明微信推送已经接通。"); err != nil {
		return infraerrors.BadRequest("PUSH_TEST_FAILED", "推送失败："+err.Error())
	}
	return nil
}
```

`backend/internal/service/push_notification_service.go`（本任务的最小版本）：

```go
package service

import (
	"context"
	"log/slog"
	"time"
)

type PushSender interface {
	Send(ctx context.Context, sendKey, title, desp string) error
}

type PushNotificationStore interface {
	IncrSent(ctx context.Context, day, category string) (int64, error)
	GetSent(ctx context.Context, day string) (map[string]int64, error)
	TryOnce(ctx context.Context, key string, ttl time.Duration) (bool, error)
}

type PushNotificationService struct {
	settingRepo SettingRepository
	store       PushNotificationStore
	sender      PushSender
	now         func() time.Time
}

func NewPushNotificationService(settingRepo SettingRepository, store PushNotificationStore, sender PushSender) *PushNotificationService {
	return &PushNotificationService{settingRepo: settingRepo, store: store, sender: sender, now: time.Now}
}

// loadConfig never fails: on error it returns a disabled default so callers simply skip pushing.
func (s *PushNotificationService) loadConfig(ctx context.Context) *PushNotificationConfig {
	cfg, err := s.readConfig(ctx)
	if err != nil {
		slog.Warn("push_notification.config_load_failed", "error", err)
		return defaultPushNotificationConfig()
	}
	return cfg
}
```

- [ ] **Step 6: 实现 Redis 存储**

`backend/internal/repository/push_notification_store.go`：

```go
package repository

import (
	"context"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	pushNotificationSentKeyPrefix = "push_notification:sent:"
	pushNotificationSentTTL       = 8 * 24 * time.Hour
)

type pushNotificationStore struct {
	rdb *redis.Client
}

func NewPushNotificationStore(rdb *redis.Client) service.PushNotificationStore {
	return &pushNotificationStore{rdb: rdb}
}

func (s *pushNotificationStore) IncrSent(ctx context.Context, day, category string) (int64, error) {
	if s == nil || s.rdb == nil {
		return 0, nil
	}
	key := pushNotificationSentKeyPrefix + day
	pipe := s.rdb.TxPipeline()
	incr := pipe.HIncrBy(ctx, key, category, 1)
	pipe.Expire(ctx, key, pushNotificationSentTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

func (s *pushNotificationStore) GetSent(ctx context.Context, day string) (map[string]int64, error) {
	out := map[string]int64{}
	if s == nil || s.rdb == nil {
		return out, nil
	}
	values, err := s.rdb.HGetAll(ctx, pushNotificationSentKeyPrefix+day).Result()
	if err != nil {
		return nil, err
	}
	for k, v := range values {
		if n, parseErr := strconv.ParseInt(v, 10, 64); parseErr == nil {
			out[k] = n
		}
	}
	return out, nil
}

func (s *pushNotificationStore) TryOnce(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if s == nil || s.rdb == nil {
		return true, nil
	}
	return s.rdb.SetNX(ctx, key, "1", ttl).Result()
}
```

- [ ] **Step 7: 实现 handler 与路由**

`backend/internal/handler/admin/push_notification_handler.go`：

```go
package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type PushNotificationHandler struct {
	service *service.PushNotificationService
}

func NewPushNotificationHandler(svc *service.PushNotificationService) *PushNotificationHandler {
	return &PushNotificationHandler{service: svc}
}

type pushNotificationConfigRequest struct {
	Enabled            *bool   `json:"enabled"`
	SendKey            *string `json:"send_key"`
	ClearSendKey       bool    `json:"clear_send_key"`
	HitDailyCap        *int    `json:"hit_daily_cap"`
	HitWindowMinutes   *int    `json:"hit_window_minutes"`
	DailyReportEnabled *bool   `json:"daily_report_enabled"`
	DailyReportHour    *int    `json:"daily_report_hour"`
	OpsCriticalEnabled *bool   `json:"ops_critical_enabled"`
}

func (h *PushNotificationHandler) GetConfig(c *gin.Context) {
	cfg, err := h.service.GetConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *PushNotificationHandler) UpdateConfig(c *gin.Context) {
	var req pushNotificationConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.service.UpdateConfig(c.Request.Context(), service.UpdatePushNotificationConfigInput{
		Enabled: req.Enabled, SendKey: req.SendKey, ClearSendKey: req.ClearSendKey,
		HitDailyCap: req.HitDailyCap, HitWindowMinutes: req.HitWindowMinutes,
		DailyReportEnabled: req.DailyReportEnabled, DailyReportHour: req.DailyReportHour,
		OpsCriticalEnabled: req.OpsCriticalEnabled,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *PushNotificationHandler) SendTest(c *gin.Context) {
	if err := h.service.SendTest(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "ok"})
}
```

`routes/admin.go` 的 `registerContentModerationRoutes` 末尾（`risk.DELETE("/hashes/all", ...)` 之后）加：

```go
		risk.GET("/push/config", h.Admin.PushNotification.GetConfig)
		risk.PUT("/push/config", h.Admin.PushNotification.UpdateConfig)
		risk.POST("/push/test", h.Admin.PushNotification.SendTest)
```

`handler/handler.go` 的 `AdminHandlers` 在 `ContentModeration` 下一行加 `PushNotification *admin.PushNotificationHandler`；`handler/wire.go` 的 `ProvideAdminHandlers` 参数列表在 `contentModerationHandler *admin.ContentModerationHandler,` 后加 `pushNotificationHandler *admin.PushNotificationHandler,`，结构体字面量加 `PushNotification: pushNotificationHandler,`，ProviderSet 在 `admin.NewContentModerationHandler,` 后加 `admin.NewPushNotificationHandler,`。

- [ ] **Step 8: 接线**

`backend/internal/service/wire.go` 新增：

```go
// ProvidePushNotificationService creates the ServerChan push service.
func ProvidePushNotificationService(settingRepo SettingRepository, store PushNotificationStore) *PushNotificationService {
	return NewPushNotificationService(settingRepo, store, serverchan.NewClient(&http.Client{Timeout: 10 * time.Second}))
}
```

（import `net/http`、`time`、`github.com/Wei-Shaw/sub2api/internal/pkg/serverchan`。）ProviderSet 加 `ProvidePushNotificationService,`；`repository/wire.go` ProviderSet 加 `NewPushNotificationStore,`。

Run: `cd backend && go generate ./cmd/server`
Expected: 生成成功，`wire_gen.go` 出现 `service.ProvidePushNotificationService` 和 `admin.NewPushNotificationHandler`

- [ ] **Step 9: 运行测试确认通过**

Run: `cd backend && go test ./internal/service -run TestPushNotification -count=1 && go test ./internal/repository -run TestPushNotificationStore -count=1 && go test ./internal/handler/admin -run TestPushNotificationHandler -count=1 && go build ./...`
Expected: PASS

- [ ] **Step 10: 提交**

```bash
git add backend/internal/service/domain_constants.go backend/internal/service/push_notification_config.go backend/internal/service/push_notification_config_test.go backend/internal/service/push_notification_service.go backend/internal/repository/push_notification_store.go backend/internal/repository/push_notification_store_test.go backend/internal/handler/admin/push_notification_handler.go backend/internal/handler/admin/push_notification_handler_test.go backend/internal/handler/handler.go backend/internal/handler/wire.go backend/internal/server/routes/admin.go backend/internal/service/wire.go backend/internal/repository/wire.go backend/cmd/server/wire_gen.go
git commit -m "feat(push): Server酱推送配置与后台接口"
```

---

### Task 7: 推送发送队列、上限与系统告警推送

**Files:**
- Modify: `backend/internal/service/push_notification_service.go`
- Create: `backend/internal/service/push_notification_format.go`
- Modify: `backend/internal/service/wire.go`（`ProvidePushNotificationService` 调用 `Start()`；`ProvideContentModerationService` 增加 `push *PushNotificationService` 参数并 `svc.SetNotifier(push)`）
- Modify: `backend/cmd/server/wire.go`（`provideCleanup` 增加 `pushNotification *service.PushNotificationService` 参数与 `{"PushNotificationService", ...Stop()}` 步骤）、`backend/cmd/server/wire_gen_test.go`（positional 调用补 `nil`）
- Regenerate: `backend/cmd/server/wire_gen.go`
- Test: `backend/internal/service/push_notification_service_test.go`（新）

**Interfaces:**
- Consumes: Task 4 的 `ContentModerationNotifier`、`ModerationSystemAlert`；Task 6 的 `PushNotificationService`、`fakePushSender`、`fakePushStore`。
- Produces:
  - `type PushCategory string`，常量 `PushCategoryHit = "hit"`、`PushCategoryAlert = "alert"`、`PushCategoryOps = "ops"`、`PushCategoryReport = "report"`、`PushCategoryNotice = "notice"`
  - `type pushMessage struct { category PushCategory; title, desp string }`
  - 方法：`Start()`、`Stop()`、`enqueue(msg pushMessage) bool`、`deliver(ctx context.Context, msg pushMessage)`、`NotifySystemAlert(ctx, ModerationSystemAlert)`、`NotifyModerationHit(ctx, ModerationHitEvent)`（本任务先空实现，Task 8 补全）
  - `func formatSystemAlert(alert ModerationSystemAlert) pushMessage`
  - `func pushDay(t time.Time) string`（等于 `ContentModerationStatsDay`）

- [ ] **Step 1: 写失败的测试**

```go
package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newEnabledPushService(t *testing.T, mutate func(*PushNotificationConfig)) (*PushNotificationService, *fakePushSender, *fakePushStore) {
	t.Helper()
	cfg := defaultPushNotificationConfig()
	cfg.Enabled, cfg.SendKey = true, "SCT470902TabcDEF123"
	if mutate != nil {
		mutate(cfg)
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	sender, store := &fakePushSender{}, &fakePushStore{}
	svc := NewPushNotificationService(&contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyPushNotificationConfig: string(raw),
		SettingKeyFrontendURL:            "https://api.example.com/",
	}}, store, sender)
	svc.retryDelay = 0
	return svc, sender, store
}

func TestPushDeliverRespectsHitDailyCapAndSendsNoticeOnce(t *testing.T) {
	svc, sender, store := newEnabledPushService(t, func(c *PushNotificationConfig) { c.HitDailyCap = 2 })
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		svc.deliver(ctx, pushMessage{category: PushCategoryHit, title: "hit", desp: "d"})
	}
	sent := sender.snapshot()
	require.Len(t, sent, 3)
	require.Equal(t, "hit", sent[0].Title)
	require.Equal(t, "hit", sent[1].Title)
	require.Contains(t, sent[2].Title, "上限")
	day := pushDay(time.Now())
	counts, _ := store.GetSent(ctx, day)
	require.Equal(t, int64(5), counts["hit"])
	require.Equal(t, int64(1), counts["notice"])
}

func TestPushDeliverRetriesOnce(t *testing.T) {
	svc, sender, _ := newEnabledPushService(t, nil)
	sender.failTimes = 1
	svc.deliver(context.Background(), pushMessage{category: PushCategoryAlert, title: "a", desp: "d"})
	require.Len(t, sender.snapshot(), 1)

	sender.failTimes = 2
	svc.deliver(context.Background(), pushMessage{category: PushCategoryAlert, title: "b", desp: "d"})
	require.Len(t, sender.snapshot(), 1, "gives up after one retry")
}

func TestPushDeliverSkipsWhenDisabled(t *testing.T) {
	svc, sender, _ := newEnabledPushService(t, func(c *PushNotificationConfig) { c.Enabled = false })
	svc.deliver(context.Background(), pushMessage{category: PushCategoryAlert, title: "a", desp: "d"})
	require.Empty(t, sender.snapshot())
}

func TestPushSystemAlertFormatting(t *testing.T) {
	firing := formatSystemAlert(ModerationSystemAlert{Kind: ModerationAlertBalanceExhausted, Detail: "TypeSafe 返回 402", At: time.Now()})
	require.Equal(t, PushCategoryAlert, firing.category)
	require.Contains(t, firing.title, "【告警】")
	require.Contains(t, firing.title, "402")
	require.Contains(t, firing.desp, "放行")

	resolved := formatSystemAlert(ModerationSystemAlert{Kind: ModerationAlertBalanceExhausted, Resolved: true, At: time.Now()})
	require.Contains(t, resolved.title, "【恢复】")
}

func TestPushServiceQueueDeliversAsync(t *testing.T) {
	svc, sender, _ := newEnabledPushService(t, nil)
	svc.Start()
	defer svc.Stop()
	svc.NotifySystemAlert(context.Background(), ModerationSystemAlert{Kind: ModerationAlertFailureRate, Detail: "x", At: time.Now()})
	require.Eventually(t, func() bool { return len(sender.snapshot()) == 1 }, 2*time.Second, 10*time.Millisecond)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test ./internal/service -run 'TestPushDeliver|TestPushSystemAlert|TestPushServiceQueue' -count=1`
Expected: 编译失败（`deliver`、`retryDelay` 等不存在）

- [ ] **Step 3: 实现格式化**

`backend/internal/service/push_notification_format.go`：

```go
package service

import "fmt"

type PushCategory string

const (
	PushCategoryHit    PushCategory = "hit"
	PushCategoryAlert  PushCategory = "alert"
	PushCategoryOps    PushCategory = "ops"
	PushCategoryReport PushCategory = "report"
	PushCategoryNotice PushCategory = "notice"
)

type pushMessage struct {
	category PushCategory
	title    string
	desp     string
}

var moderationAlertTitles = map[ModerationAlertKind]string{
	ModerationAlertBalanceExhausted: "审计余额耗尽(402)",
	ModerationAlertAllKeysFrozen:    "审计Key全部冻结",
	ModerationAlertFailureRate:      "审计失败率过高",
}

func formatSystemAlert(alert ModerationSystemAlert) pushMessage {
	name := moderationAlertTitles[alert.Kind]
	if name == "" {
		name = string(alert.Kind)
	}
	at := alert.At.In(ContentModerationReportLocation()).Format("2006-01-02 15:04:05")
	if alert.Resolved {
		return pushMessage{category: PushCategoryAlert, title: "【恢复】" + name, desp: fmt.Sprintf("%s 已恢复（%s）。", name, at)}
	}
	desp := fmt.Sprintf("%s\n\n- 时间：%s\n- 详情：%s\n\n审计失败期间请求会按配置放行（fail-open），请尽快处理。", name, at, alert.Detail)
	return pushMessage{category: PushCategoryAlert, title: "【告警】" + name, desp: desp}
}

func formatHitCapNotice(cap int) pushMessage {
	return pushMessage{category: PushCategoryNotice, title: "【提示】今日命中推送已达上限",
		desp: fmt.Sprintf("今天的命中推送已达到 %d 条上限，之后的命中不再实时推送，请查看后台和明天的日报。", cap)}
}
```

- [ ] **Step 4: 实现队列与发送**

把 `push_notification_service.go` 替换为：

```go
package service

import (
	"context"
	"log/slog"
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
}

func NewPushNotificationService(settingRepo SettingRepository, store PushNotificationStore, sender PushSender) *PushNotificationService {
	return &PushNotificationService{
		settingRepo: settingRepo, store: store, sender: sender, now: time.Now,
		retryDelay: defaultPushRetryDelay, limiter: newSlidingWindowLimiter(pushPerMinuteLimit, time.Minute),
		queue: make(chan pushMessage, pushQueueSize), stop: make(chan struct{}),
	}
}

func pushDay(t time.Time) string { return ContentModerationStatsDay(t) }

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
			ctx, cancel := context.WithTimeout(context.Background(), 2*pushSendTimeout)
			s.deliver(ctx, msg)
			cancel()
		case <-ticker.C:
			s.flushHitWindows(context.Background())
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
	day := pushDay(s.now())
	if s.store != nil {
		count, err := s.store.IncrSent(ctx, day, string(msg.category))
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

// NotifyModerationHit is completed in Task 8.
func (s *PushNotificationService) NotifyModerationHit(_ context.Context, _ ModerationHitEvent) {}

// flushHitWindows is completed in Task 8.
func (s *PushNotificationService) flushHitWindows(_ context.Context) {}
```

（删除 Task 6 的最小版本里重复的 `loadConfig`、接口定义。）

- [ ] **Step 5: 接线与关停**

`service/wire.go`：

```go
func ProvidePushNotificationService(settingRepo SettingRepository, store PushNotificationStore) *PushNotificationService {
	svc := NewPushNotificationService(settingRepo, store, serverchan.NewClient(&http.Client{Timeout: 10 * time.Second}))
	svc.Start()
	return svc
}
```

`ProvideContentModerationService` 在参数末尾加 `push *PushNotificationService`，函数体在 `svc.SetStatsStore(statsStore)` 后加：

```go
	if push != nil {
		svc.SetNotifier(push)
	}
```

`cmd/server/wire.go` 的 `provideCleanup` 参数末尾（`pluginManager *service.PluginManager,` 之后）加 `pushNotification *service.PushNotificationService,`，`parallelSteps` 加：

```go
			{"PushNotificationService", func() error {
				if pushNotification != nil {
					pushNotification.Stop()
				}
				return nil
			}},
```

`cmd/server/wire_gen_test.go` 中 `provideCleanup(...)` 调用的最后一个实参后加 `nil, // pushNotification`。

Run: `cd backend && go generate ./cmd/server && go build ./...`
Expected: 编译通过

- [ ] **Step 6: 运行测试确认通过**

Run: `cd backend && go test ./internal/service -run 'TestPush|TestContentModeration' -count=1 && go test ./cmd/server -count=1`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add backend/internal/service/push_notification_service.go backend/internal/service/push_notification_format.go backend/internal/service/push_notification_service_test.go backend/internal/service/wire.go backend/cmd/server/wire.go backend/cmd/server/wire_gen.go backend/cmd/server/wire_gen_test.go
git commit -m "feat(push): 推送队列、每日上限、重试与审计系统告警推送"
```

---

### Task 8: 审计命中推送（按用户合并）

**Files:**
- Create: `backend/internal/service/moderation_hit_aggregator.go`
- Modify: `backend/internal/service/push_notification_service.go`（`NotifyModerationHit`、`flushHitWindows`、字段 `hits`）
- Modify: `backend/internal/service/push_notification_format.go`（命中与合并消息）
- Modify: `backend/internal/service/content_moderation.go`（`persistContentModerationLog` 调用 `notifyHit`）
- Test: `backend/internal/service/moderation_hit_aggregator_test.go`（新）、`backend/internal/service/content_moderation_hit_notify_test.go`（新）

**Interfaces:**
- Consumes: Task 4 的 `ModerationHitEvent`、`fakeModerationNotifier`；Task 7 的 `pushMessage`、`newEnabledPushService`；Task 2 的 `newOpenAIModerationTestService`、`moderationChatBody`。
- Produces:
  - `type moderationHitAggregator struct`，方法 `record(ev ModerationHitEvent, window time.Duration) (immediate bool, expired *moderationHitSummary)`、`flush(now time.Time, window time.Duration) []moderationHitSummary`
  - `type moderationHitSummary struct { UserID int64; UserEmail string; Count int; TopCategory string; Observe bool; WindowStart time.Time; Window time.Duration }`
  - `func formatModerationHit(ev ModerationHitEvent, link string) pushMessage`、`func formatHitSummary(sum moderationHitSummary) pushMessage`
  - `func (s *PushNotificationService) adminLogLink(ctx context.Context, requestID string) string`
  - `func (s *ContentModerationService) notifyHit(log *ContentModerationLog, inputHash string)`

- [ ] **Step 1: 写聚合器失败测试**

```go
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func hitAt(userID int64, at time.Time, action, category string) ModerationHitEvent {
	return ModerationHitEvent{UserID: userID, UserEmail: "u@example.com", Category: category, Score: 0.9, Action: action, At: at}
}

func TestModerationHitAggregatorFirstHitImmediateRestMerged(t *testing.T) {
	var a moderationHitAggregator
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	window := 30 * time.Minute

	immediate, expired := a.record(hitAt(1, t0, ContentModerationActionBlock, "sexual"), window)
	require.True(t, immediate)
	require.Nil(t, expired)
	immediate, _ = a.record(hitAt(1, t0.Add(time.Minute), ContentModerationActionBlock, "sexual"), window)
	require.False(t, immediate)
	immediate, _ = a.record(hitAt(1, t0.Add(2*time.Minute), ContentModerationActionHashBlock, "hash"), window)
	require.False(t, immediate)
	immediate, _ = a.record(hitAt(1, t0.Add(3*time.Minute), ContentModerationActionBlock, "violence"), window)
	require.False(t, immediate)

	require.Empty(t, a.flush(t0.Add(29*time.Minute), window))
	sums := a.flush(t0.Add(30*time.Minute), window)
	require.Len(t, sums, 1)
	require.Equal(t, 3, sums[0].Count)
	require.Equal(t, "sexual", sums[0].TopCategory)
	require.Empty(t, a.flush(t0.Add(31*time.Minute), window), "window removed after flush")
}

func TestModerationHitAggregatorHashBlockNeverImmediate(t *testing.T) {
	var a moderationHitAggregator
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	window := 30 * time.Minute

	immediate, _ := a.record(hitAt(2, t0, ContentModerationActionHashBlock, "hash"), window)
	require.False(t, immediate)
	immediate, _ = a.record(hitAt(2, t0.Add(time.Minute), ContentModerationActionBlock, "sexual"), window)
	require.True(t, immediate, "first real hit in the window is still pushed")
}

func TestModerationHitAggregatorReturnsExpiredWindowOnNewHit(t *testing.T) {
	var a moderationHitAggregator
	t0 := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	window := 30 * time.Minute
	a.record(hitAt(3, t0, ContentModerationActionBlock, "sexual"), window)
	a.record(hitAt(3, t0.Add(time.Minute), ContentModerationActionBlock, "sexual"), window)

	immediate, expired := a.record(hitAt(3, t0.Add(40*time.Minute), ContentModerationActionBlock, "sexual"), window)
	require.True(t, immediate)
	require.NotNil(t, expired)
	require.Equal(t, 1, expired.Count)
}

func TestPushNotifyModerationHitPushesFirstAndSummary(t *testing.T) {
	svc, sender, _ := newEnabledPushService(t, nil)
	t0 := time.Now()
	svc.now = func() time.Time { return t0 }
	ctx := context.Background()
	ev := ModerationHitEvent{UserID: 9, UserEmail: "nine@example.com", GroupName: "定制0.8", Model: "claude-opus-5-5", Category: "sexual", Score: 0.93, Action: ContentModerationActionBlock, InputHash: "abc123", RequestID: "req-1", At: t0}

	svc.NotifyModerationHit(ctx, ev)
	ev.At, ev.RequestID = t0.Add(time.Minute), "req-2"
	svc.NotifyModerationHit(ctx, ev)
	drainPushQueue(ctx, svc)
	sent := sender.snapshot()
	require.Len(t, sent, 1)
	require.Contains(t, sent[0].Title, "【拦截】")
	require.Contains(t, sent[0].Desp, "nine@example.com")
	require.Contains(t, sent[0].Desp, "abc123")
	require.Contains(t, sent[0].Desp, "https://api.example.com/admin/risk-control?search=req-1")

	svc.now = func() time.Time { return t0.Add(31 * time.Minute) }
	svc.flushHitWindows(ctx)
	drainPushQueue(ctx, svc)
	sent = sender.snapshot()
	require.Len(t, sent, 2)
	require.Contains(t, sent[1].Title, "又命中1次")
}

func TestFormatModerationHitObserveLabel(t *testing.T) {
	msg := formatModerationHit(ModerationHitEvent{UserEmail: "o@example.com", Category: "sexual", Score: 0.9, Action: ContentModerationActionAllow, Observe: true}, "")
	require.Contains(t, msg.title, "【观察】")
	require.Contains(t, msg.desp, "若开拦会被拦")
	require.NotContains(t, msg.desp, "后台查看")
}

// drainPushQueue delivers everything queued without running the background loop.
func drainPushQueue(ctx context.Context, svc *PushNotificationService) {
	for {
		select {
		case msg := <-svc.queue:
			svc.deliver(ctx, msg)
		default:
			return
		}
	}
}
```

- [ ] **Step 2: 写审计侧失败测试**

`content_moderation_hit_notify_test.go`：

```go
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestContentModerationPersistNotifiesFlaggedHit(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.99, nil)
	notifier := &fakeModerationNotifier{}
	svc.SetNotifier(notifier)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		RequestID: "req-hit", UserID: 7, UserEmail: "seven@example.com", GroupName: "x1", Model: "claude-opus-5-5",
		Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("bad prompt"),
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)

	// pre_block persists flagged logs through the async queue
	task := <-svc.asyncQueue
	svc.persistContentModerationLog(context.Background(), task.config, task.log, task.inputHash, task.recordHash, task.applySideEffects)

	require.Eventually(t, func() bool { return len(notifier.snapshotHits()) == 1 }, time.Second, 10*time.Millisecond)
	hit := notifier.snapshotHits()[0]
	require.Equal(t, int64(7), hit.UserID)
	require.Equal(t, "seven@example.com", hit.UserEmail)
	require.Equal(t, "sexual", hit.Category)
	require.Equal(t, ContentModerationActionBlock, hit.Action)
	require.Equal(t, "req-hit", hit.RequestID)
	require.Equal(t, task.inputHash, hit.InputHash)
	require.False(t, hit.Observe)
}

func TestContentModerationPersistSkipsNonFlagged(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.0, func(cfg *ContentModerationConfig) { cfg.RecordNonHits = true })
	notifier := &fakeModerationNotifier{}
	svc.SetNotifier(notifier)
	_, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 7, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("hello")})
	require.NoError(t, err)
	task := <-svc.asyncQueue
	require.False(t, task.log.Flagged)
	svc.persistContentModerationLog(context.Background(), task.config, task.log, task.inputHash, task.recordHash, task.applySideEffects)
	require.Empty(t, notifier.snapshotHits())
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd backend && go test ./internal/service -run 'TestModerationHitAggregator|TestPushNotifyModerationHit|TestFormatModerationHit|TestContentModerationPersist' -count=1`
Expected: 编译失败

- [ ] **Step 4: 实现聚合器**

`backend/internal/service/moderation_hit_aggregator.go`：

```go
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

// moderationHitAggregator keeps one in-memory window per user (per instance).
type moderationHitAggregator struct {
	mu      sync.Mutex
	windows map[int64]*moderationHitWindow
}

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
```

- [ ] **Step 5: 实现命中消息格式**

追加到 `push_notification_format.go`（import 增加 `strings`）：

```go
func moderationHitLabel(ev ModerationHitEvent) string {
	switch {
	case ev.Observe:
		return "观察"
	case ev.Action == ContentModerationActionAllow:
		return "命中"
	default:
		return "拦截"
	}
}

func formatModerationHit(ev ModerationHitEvent, link string) pushMessage {
	label := moderationHitLabel(ev)
	var b strings.Builder
	b.WriteString("| 项目 | 内容 |\n|---|---|\n")
	fmt.Fprintf(&b, "| 用户 | %s（ID %d） |\n", ev.UserEmail, ev.UserID)
	fmt.Fprintf(&b, "| 分组 | %s |\n| 模型 | %s |\n", ev.GroupName, ev.Model)
	fmt.Fprintf(&b, "| 类别 | %s（%.2f） |\n", ev.Category, ev.Score)
	handling := label
	if ev.Observe {
		handling = "观察名单用户，本次未拦截（若开拦会被拦）"
	}
	fmt.Fprintf(&b, "| 处理 | %s |\n| request_id | %s |\n| 内容哈希 | %s |\n", handling, ev.RequestID, ev.InputHash)
	if link != "" {
		fmt.Fprintf(&b, "\n[在后台查看](%s)\n", link)
	}
	title := fmt.Sprintf("【%s】%s %.2f %s", label, ev.Category, ev.Score, ev.UserEmail)
	return pushMessage{category: PushCategoryHit, title: title, desp: b.String()}
}

func formatHitSummary(sum moderationHitSummary) pushMessage {
	start := sum.WindowStart.In(ContentModerationReportLocation()).Format("15:04")
	note := ""
	if sum.Observe {
		note = "（观察名单用户，未拦截）"
	}
	return pushMessage{
		category: PushCategoryHit,
		title:    fmt.Sprintf("【合并】%s 又命中%d次", sum.UserEmail, sum.Count),
		desp:     fmt.Sprintf("用户 %s（ID %d）在 %s 起的 %d 分钟内又命中 %d 次，主要类别 %s%s。", sum.UserEmail, sum.UserID, start, int(sum.Window.Minutes()), sum.Count, sum.TopCategory, note),
	}
}
```

- [ ] **Step 6: 实现推送侧逻辑**

`push_notification_service.go`：结构体加字段 `hits moderationHitAggregator`；import 加 `net/url`、`strings`；把 Task 7 的两个空实现替换为：

```go
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

func (s *PushNotificationService) adminLogLink(ctx context.Context, requestID string) string {
	if s == nil || s.settingRepo == nil || requestID == "" {
		return ""
	}
	base, err := s.settingRepo.GetValue(ctx, SettingKeyFrontendURL)
	if err != nil || strings.TrimSpace(base) == "" {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(base), "/") + "/admin/risk-control?search=" + url.QueryEscape(requestID)
}
```

- [ ] **Step 7: 审计侧调用**

`content_moderation.go` 的 `persistContentModerationLog` 中，在 `if s.repo != nil {` 之前加 `s.notifyHit(log, hashText)`，并新增：

```go
// notifyHit forwards flagged audits to the notifier; it never includes the audited text.
func (s *ContentModerationService) notifyHit(log *ContentModerationLog, inputHash string) {
	if s == nil || s.notifier == nil || log == nil || !log.Flagged {
		return
	}
	var userID int64
	if log.UserID != nil {
		userID = *log.UserID
	}
	s.notifier.NotifyModerationHit(context.Background(), ModerationHitEvent{
		UserID: userID, UserEmail: log.UserEmail, GroupName: log.GroupName, Model: log.Model,
		Category: log.HighestCategory, Score: log.HighestScore, Action: log.Action,
		Observe:   log.Mode == ContentModerationModeRiskControlLogOnly,
		InputHash: inputHash, RequestID: log.RequestID, At: time.Now(),
	})
}
```

- [ ] **Step 8: 运行测试确认通过**

Run: `cd backend && go test ./internal/service -run 'TestModerationHit|TestPush|TestFormatModerationHit|TestContentModeration' -count=1`
Expected: PASS

- [ ] **Step 9: 提交**

```bash
git add backend/internal/service/moderation_hit_aggregator.go backend/internal/service/moderation_hit_aggregator_test.go backend/internal/service/push_notification_service.go backend/internal/service/push_notification_format.go backend/internal/service/content_moderation.go backend/internal/service/content_moderation_hit_notify_test.go
git commit -m "feat(push): 审计命中按用户 30 分钟合并推送"
```

---

### Task 9: ops P0 告警推送

**Files:**
- Modify: `backend/internal/service/ops_alert_evaluator_service.go`（结构体字段 :34-58；`evaluateOnce` :294-298；新增 setter）
- Modify: `backend/internal/service/push_notification_service.go`（`NotifyOpsAlert`）
- Modify: `backend/internal/service/push_notification_format.go`（`formatOpsAlert`）
- Modify: `backend/internal/service/wire.go`（`ProvideOpsAlertEvaluatorService` 加 `push *PushNotificationService` 参数，`Start()` 前调用 setter）
- Regenerate: `backend/cmd/server/wire_gen.go`
- Test: `backend/internal/service/push_notification_ops_test.go`（新）

**Interfaces:**
- Consumes: `OpsAlertRule{ID, Name, Description, Severity, MetricType, Operator, Threshold}`、`OpsAlertEvent{Title, Description, MetricValue *float64, ThresholdValue *float64, FiredAt}`（`ops_alert_models.go:16-60`）；Task 7 的 `newEnabledPushService`、`drainPushQueue`（Task 8）。
- Produces:
  - `type OpsCriticalAlertNotifier interface { NotifyOpsAlert(ctx context.Context, rule *OpsAlertRule, event *OpsAlertEvent) }`
  - `func (s *OpsAlertEvaluatorService) SetCriticalAlertNotifier(n OpsCriticalAlertNotifier)`
  - `func isOpsCriticalSeverity(severity string) bool`（`P0` 不区分大小写）
  - `func formatOpsAlert(rule *OpsAlertRule, event *OpsAlertEvent) pushMessage`

- [ ] **Step 1: 写失败的测试**

```go
package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIsOpsCriticalSeverity(t *testing.T) {
	require.True(t, isOpsCriticalSeverity("P0"))
	require.True(t, isOpsCriticalSeverity(" p0 "))
	require.False(t, isOpsCriticalSeverity("P1"))
	require.False(t, isOpsCriticalSeverity(""))
}

func TestPushNotifyOpsAlertRespectsSwitch(t *testing.T) {
	value, threshold := 0.42, 0.2
	rule := &OpsAlertRule{ID: 3, Name: "错误率极高", Severity: "P0", MetricType: "error_rate"}
	event := &OpsAlertEvent{Title: "错误率极高", Description: "5 分钟错误率 42%", MetricValue: &value, ThresholdValue: &threshold, FiredAt: time.Now()}

	svc, sender, _ := newEnabledPushService(t, nil)
	svc.NotifyOpsAlert(context.Background(), rule, event)
	drainPushQueue(context.Background(), svc)
	sent := sender.snapshot()
	require.Len(t, sent, 1)
	require.Contains(t, sent[0].Title, "【P0】")
	require.Contains(t, sent[0].Desp, "0.42")

	off, offSender, _ := newEnabledPushService(t, func(c *PushNotificationConfig) { c.OpsCriticalEnabled = false })
	off.NotifyOpsAlert(context.Background(), rule, event)
	drainPushQueue(context.Background(), off)
	require.Empty(t, offSender.snapshot())

	raw, _ := json.Marshal(sent[0])
	require.NotContains(t, string(raw), "SCT470902TabcDEF123"[3:], "key never in content")
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test ./internal/service -run 'TestIsOpsCriticalSeverity|TestPushNotifyOpsAlert' -count=1`
Expected: 编译失败

- [ ] **Step 3: 实现**

`push_notification_format.go` 追加：

```go
func formatOpsAlert(rule *OpsAlertRule, event *OpsAlertEvent) pushMessage {
	var b strings.Builder
	fmt.Fprintf(&b, "- 规则：%s（%s）\n", rule.Name, rule.MetricType)
	if event.Description != "" {
		fmt.Fprintf(&b, "- 描述：%s\n", event.Description)
	}
	if event.MetricValue != nil {
		fmt.Fprintf(&b, "- 当前值：%.4g\n", *event.MetricValue)
	}
	if event.ThresholdValue != nil {
		fmt.Fprintf(&b, "- 阈值：%.4g\n", *event.ThresholdValue)
	}
	fmt.Fprintf(&b, "- 触发时间：%s\n", event.FiredAt.In(ContentModerationReportLocation()).Format("2006-01-02 15:04:05"))
	return pushMessage{category: PushCategoryOps, title: "【P0】" + event.Title, desp: b.String()}
}
```

`push_notification_service.go` 追加：

```go
func (s *PushNotificationService) NotifyOpsAlert(ctx context.Context, rule *OpsAlertRule, event *OpsAlertEvent) {
	if s == nil || rule == nil || event == nil {
		return
	}
	if !s.loadConfig(ctx).OpsCriticalEnabled {
		return
	}
	s.enqueue(formatOpsAlert(rule, event))
}
```

`ops_alert_evaluator_service.go`：结构体加字段 `criticalNotifier OpsCriticalAlertNotifier`，文件中新增：

```go
type OpsCriticalAlertNotifier interface {
	NotifyOpsAlert(ctx context.Context, rule *OpsAlertRule, event *OpsAlertEvent)
}

func (s *OpsAlertEvaluatorService) SetCriticalAlertNotifier(n OpsCriticalAlertNotifier) {
	if s != nil {
		s.criticalNotifier = n
	}
}

func isOpsCriticalSeverity(severity string) bool {
	return strings.EqualFold(strings.TrimSpace(severity), "P0")
}
```

`evaluateOnce` 中把

```go
		if created != nil && created.ID > 0 {
			if s.maybeSendAlertEmail(ctx, runtimeCfg, rule, created) {
				emailsSent++
			}
		}
```

改成

```go
		if created != nil && created.ID > 0 {
			if s.maybeSendAlertEmail(ctx, runtimeCfg, rule, created) {
				emailsSent++
			}
			if s.criticalNotifier != nil && isOpsCriticalSeverity(rule.Severity) {
				s.criticalNotifier.NotifyOpsAlert(ctx, rule, created)
			}
		}
```

`service/wire.go` 的 `ProvideOpsAlertEvaluatorService` 参数末尾加 `push *PushNotificationService`，`svc.Start()` 前加：

```go
	if push != nil {
		svc.SetCriticalAlertNotifier(push)
	}
```

Run: `cd backend && go generate ./cmd/server && go build ./...`

- [ ] **Step 4: 运行测试确认通过**

Run: `cd backend && go test ./internal/service -run 'TestIsOpsCriticalSeverity|TestPushNotifyOpsAlert|TestOpsAlert' -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/internal/service/ops_alert_evaluator_service.go backend/internal/service/push_notification_service.go backend/internal/service/push_notification_format.go backend/internal/service/push_notification_ops_test.go backend/internal/service/wire.go backend/cmd/server/wire_gen.go
git commit -m "feat(push): ops P0 告警推送到微信"
```

---

### Task 10: 审计观察名单（取代审计对风控白名单的读取）

**Files:**
- Modify: `backend/internal/service/content_moderation.go`（`ContentModerationConfig` :142、`ContentModerationConfigView` :180、`UpdateContentModerationConfigInput` :276、`Check` :855、`UpdateConfig` :628、`contentModerationRuntimeSnapshot` :549、`refreshRuntimeSnapshot` :1602、`replaceRuntimeConfig` :1648、`defaultContentModerationConfig` :2139、`cloneContentModerationConfig` :2173、`normalize` :2193、`configView` :2463）
- Modify: `backend/internal/handler/admin/content_moderation_handler.go`（请求 DTO :22-60、`UpdateConfig` 映射 :93-133）
- Create: `backend/migrations/248a_content_moderation_observe_users.sql`
- Create: `backend/migrations/content_moderation_observe_users_migration_test.go`
- Modify: `backend/internal/service/risk_control_allowlist_test.go`（改写两个审计相关测试）
- Test: `backend/internal/service/content_moderation_observe_test.go`（新）

**Interfaces:**
- Produces：配置字段 `ObserveUserIDs []int64`（json `observe_user_ids`，视图和更新入参同名；更新入参类型 `*[]int64`）；`contentModerationRuntimeSnapshot.observeUsers map[int64]struct{}`（替换 `allowlistedUsers`）；`func contentModerationObserveSet(cfg *ContentModerationConfig) map[int64]struct{}`。
- Consumes：Task 2 的 `newOpenAIModerationTestService`、`moderationChatBody`。

- [ ] **Step 1: 写失败的测试**

`backend/internal/service/content_moderation_observe_test.go`：

```go
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationObserveUserIsAuditedButNotBlocked(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.99, func(cfg *ContentModerationConfig) {
		cfg.ObserveUserIDs = []int64{12}
	})
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 12, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("bad prompt")})
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)

	task := <-svc.asyncQueue
	require.Equal(t, ContentModerationModeRiskControlLogOnly, task.log.Mode)
	require.True(t, task.log.Flagged)
}

func TestContentModerationCyberAllowlistNoLongerExemptsModeration(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.99, nil)
	svc.settingRepo.(*contentModerationTestSettingRepo).values[SettingKeyCyberPolicyUserAllowlist] = "12"

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 12, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("bad prompt")})

	require.NoError(t, err)
	require.True(t, decision.Blocked, "cyber allowlist only governs OpenAI cyber_policy now")
}

func TestContentModerationObserveUsersRoundTripThroughUpdateConfig(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.99, nil)
	ctx := context.Background()
	_, err := svc.loadRuntimeSnapshot(ctx)
	require.NoError(t, err)

	ids := []int64{22, 16, 0, 16, -3, 2}
	view, err := svc.UpdateConfig(ctx, UpdateContentModerationConfigInput{ObserveUserIDs: &ids})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{2, 16, 22}, view.ObserveUserIDs)
	require.Contains(t, svc.runtimeSnapshot.Load().observeUsers, int64(16))

	empty := []int64{}
	view, err = svc.UpdateConfig(ctx, UpdateContentModerationConfigInput{ObserveUserIDs: &empty})
	require.NoError(t, err)
	require.Empty(t, view.ObserveUserIDs)
	require.Empty(t, svc.runtimeSnapshot.Load().observeUsers)
}
```

`backend/migrations/content_moderation_observe_users_migration_test.go`：

```go
package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationObserveUsersMigration(t *testing.T) {
	raw, err := FS.ReadFile("248a_content_moderation_observe_users.sql")
	require.NoError(t, err)
	sql := string(raw)
	require.Contains(t, sql, "'content_moderation_config'")
	require.Contains(t, sql, "'cyber_policy_user_allowlist'")
	require.Contains(t, sql, "observe_user_ids")
	require.Contains(t, sql, "NOT (cfg.value::jsonb ? 'observe_user_ids')", "must be idempotent and never overwrite an explicit list")
	upper := strings.ToUpper(sql)
	for _, disallowed := range []string{"DELETE FROM", "DROP ", "TRUNCATE"} {
		require.NotContains(t, upper, disallowed)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test ./internal/service -run 'TestContentModerationObserve|TestContentModerationCyberAllowlist' -count=1; go test ./migrations -run TestContentModerationObserveUsersMigration -count=1`
Expected: 编译失败（`ObserveUserIDs` undefined）；迁移测试 FAIL（文件不存在）

- [ ] **Step 3: 增加配置字段（按 8 层清单逐一加）**

`content_moderation.go`：

1. `ContentModerationConfig` 在 `GroupIDs` 后加：
   ```go
   	// ObserveUserIDs are audited and notified but never blocked (staged rollout of enforcement).
   	ObserveUserIDs []int64 `json:"observe_user_ids"`
   ```
2. `ContentModerationConfigView` 在 `GroupIDs` 后加 `ObserveUserIDs []int64 \`json:"observe_user_ids"\``。
3. `UpdateContentModerationConfigInput` 在 `GroupIDs` 后加 `ObserveUserIDs *[]int64 \`json:"observe_user_ids"\``。
4. `defaultContentModerationConfig` 加 `ObserveUserIDs: []int64{},`。
5. `cloneContentModerationConfig` 加 `clone.ObserveUserIDs = append([]int64(nil), cfg.ObserveUserIDs...)`。
6. `normalize` 在 `cfg.GroupIDs = normalizeInt64IDs(cfg.GroupIDs)` 后加 `cfg.ObserveUserIDs = normalizeInt64IDs(cfg.ObserveUserIDs)`。
7. `configView` 加 `ObserveUserIDs: append([]int64(nil), cfg.ObserveUserIDs...),`。
8. `UpdateConfig` 在 `GroupIDs` 的 apply 块后加：
   ```go
   	if input.ObserveUserIDs != nil {
   		cfg.ObserveUserIDs = normalizeInt64IDs(*input.ObserveUserIDs)
   	}
   ```

`content_moderation_handler.go`：请求结构体在 `GroupIDs` 后加 `ObserveUserIDs *[]int64 \`json:"observe_user_ids"\``；`UpdateConfig` 映射加 `ObserveUserIDs: req.ObserveUserIDs,`。

- [ ] **Step 4: 运行时快照改读观察名单**

1. `contentModerationRuntimeSnapshot` 把 `allowlistedUsers map[int64]struct{}` 改名为 `observeUsers map[int64]struct{}`。
2. 新增：
   ```go
   func contentModerationObserveSet(cfg *ContentModerationConfig) map[int64]struct{} {
   	set := make(map[int64]struct{}, len(cfg.ObserveUserIDs))
   	for _, id := range cfg.ObserveUserIDs {
   		set[id] = struct{}{}
   	}
   	return set
   }
   ```
3. `refreshRuntimeSnapshot`：`GetMultiple` 只取 `SettingKeyRiskControlEnabled` 和 `SettingKeyContentModerationConfig`；删除 `ParseCyberPolicyUserAllowlist` 调用及其错误返回；digest 命中分支用 `observeUsers: current.observeUsers`；新配置分支用 `observeUsers: contentModerationObserveSet(cfg)`。
4. `replaceRuntimeConfig` 的 Store 用 `observeUsers: contentModerationObserveSet(config)`（不再沿用 `current.allowlistedUsers`）。
5. `Check` 中把 `_, input.riskControlLogOnly = runtimeSnapshot.allowlistedUsers[input.UserID]` 改成 `_, input.riskControlLogOnly = runtimeSnapshot.observeUsers[input.UserID]`。

Run: `cd backend && go build ./... && grep -rn "allowlistedUsers" internal/service/content_moderation*.go`
Expected: 编译通过，grep 无输出（`cyber_policy_allowlist.go` 和 `setting_gateway_runtime.go` 里的 cyber 用法保持不变）

- [ ] **Step 5: 改写旧测试**

`risk_control_allowlist_test.go`：
- `TestRiskControlAllowlistAuditsWithoutLocalPenalties` 改名为 `TestContentModerationObserveUsersAuditWithoutLocalPenalties`：去掉 settings 里的 `SettingKeyCyberPolicyUserAllowlist: "12"`，在 `cfg.Mode = ...` 之后加 `cfg.ObserveUserIDs = []int64{12}`，其余断言保持不变。
- 删除 `failingModerationAllowlistRepo` 和 `TestRiskControlAllowlistSnapshotRetainsMembershipOnFailure`（观察名单现在跟随配置快照，失败保留行为已由 `content_moderation_runtime_cache_test.go` 覆盖）。
- `TestRiskControlAllowlistRetainsLastGoodValueAndRetries`（cyber 设置缓存）保持不动。

- [ ] **Step 6: 写迁移**

`backend/migrations/248a_content_moderation_observe_users.sql`：

```sql
-- 审计观察名单：把现有风控白名单复制进内容审计配置的 observe_user_ids，
-- 让升级后"只记录不拦截"的行为保持不变；风控白名单此后只管 OpenAI cyber_policy。
-- 只在配置里还没有 observe_user_ids 时写入，可重复执行；配置不是合法 JSON 时跳过。
DO $$
BEGIN
    UPDATE settings AS cfg
    SET value = jsonb_set(
            cfg.value::jsonb,
            '{observe_user_ids}',
            COALESCE((
                SELECT jsonb_agg(DISTINCT id::bigint ORDER BY id::bigint)
                FROM regexp_split_to_table(trim(allow.value), '[,[:space:]]+') AS id
                WHERE id ~ '^[0-9]+$' AND id::bigint > 0
            ), '[]'::jsonb)
        )::text,
        updated_at = NOW()
    FROM settings AS allow
    WHERE cfg.key = 'content_moderation_config'
      AND allow.key = 'cyber_policy_user_allowlist'
      AND trim(cfg.value) <> ''
      AND NOT (cfg.value::jsonb ? 'observe_user_ids');
EXCEPTION
    WHEN invalid_text_representation THEN
        RAISE NOTICE 'content_moderation_config is not valid JSON; observe_user_ids not migrated';
END $$;
```

- [ ] **Step 7: 运行测试确认通过**

Run: `cd backend && go test ./internal/service -run 'TestContentModeration|TestRiskControl' -count=1 && go test ./migrations -count=1 && go test ./internal/handler/admin -count=1`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add backend/internal/service/content_moderation.go backend/internal/service/content_moderation_observe_test.go backend/internal/service/risk_control_allowlist_test.go backend/internal/handler/admin/content_moderation_handler.go backend/migrations/248a_content_moderation_observe_users.sql backend/migrations/content_moderation_observe_users_migration_test.go
git commit -m "feat(moderation): 新增审计观察名单，风控白名单不再豁免内容审计"
```

---

### Task 11: 审计结果缓存（24h）

**Files:**
- Create: `backend/internal/service/content_moderation_verdict_cache.go`
- Create: `backend/internal/repository/content_moderation_verdict_cache.go`
- Modify: `backend/internal/service/content_moderation.go`（配置字段 `VerdictCacheTTLMinutes` 的 8 层；结构体字段 `verdictCache`；`checkSync`）
- Modify: `backend/internal/handler/admin/content_moderation_handler.go`（DTO + 映射）
- Modify: `backend/internal/service/wire.go`（`ProvideContentModerationService` 加 `verdictCache ContentModerationVerdictCache` 参数并 `svc.SetVerdictCache(verdictCache)`）、`backend/internal/repository/wire.go`（加 `NewContentModerationVerdictCache`）
- Regenerate: `backend/cmd/server/wire_gen.go`
- Test: `backend/internal/service/content_moderation_verdict_cache_test.go`（新）、`backend/internal/repository/content_moderation_verdict_cache_test.go`（新）

**Interfaces:**
- Produces:
  - `type ContentModerationVerdictCache interface { Get(ctx context.Context, key string) (map[string]float64, bool, error); Set(ctx context.Context, key string, scores map[string]float64, ttl time.Duration) error }`
  - `func (s *ContentModerationService) SetVerdictCache(c ContentModerationVerdictCache)`
  - `func contentModerationVerdictCacheKey(cfg *ContentModerationConfig, inputHash string) string`
  - 配置字段 `VerdictCacheTTLMinutes int`（json `verdict_cache_ttl_minutes`，默认 1440，0 = 关闭，上限 10080）
  - `repository.NewContentModerationVerdictCache(rdb *redis.Client) service.ContentModerationVerdictCache`
- Consumes: Task 1 的 `EngineMeta.Cached`；Task 2 的 `fakeModerationStatsStore`、`moderationChatBody`。

- [ ] **Step 1: 写服务层失败测试**

```go
package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memoryVerdictCache struct {
	mu     sync.Mutex
	values map[string]map[string]float64
	ttls   map[string]time.Duration
}

func (m *memoryVerdictCache) Get(_ context.Context, key string) (map[string]float64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[key]
	return v, ok, nil
}

func (m *memoryVerdictCache) Set(_ context.Context, key string, scores map[string]float64, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values, m.ttls = map[string]map[string]float64{}, map[string]time.Duration{}
	}
	m.values[key], m.ttls[key] = scores, ttl
	return nil
}

func newCountingModerationService(t *testing.T, statuses []int, mutate func(*ContentModerationConfig)) (*ContentModerationService, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1)) - 1
		if n < len(statuses) && statuses[n] != http.StatusOK {
			w.WriteHeader(statuses[n])
			return
		}
		_ = json.NewEncoder(w).Encode(moderationAPIResponse{Results: []moderationAPIResult{{CategoryScores: map[string]float64{"sexual": 0.1}}}})
	}))
	t.Cleanup(server.Close)
	cfg := defaultContentModerationConfig()
	cfg.Enabled, cfg.Mode, cfg.RecordNonHits = true, ContentModerationModePreBlock, true
	cfg.EmailOnHit, cfg.AutoBanEnabled = false, false
	cfg.BaseURL, cfg.APIKeys, cfg.RetryCount = server.URL, []string{"sk-test"}, 0
	if mutate != nil {
		mutate(cfg)
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	svc := &ContentModerationService{
		settingRepo: &contentModerationTestSettingRepo{values: map[string]string{SettingKeyRiskControlEnabled: "true", SettingKeyContentModerationConfig: string(raw)}},
		repo: &contentModerationTestRepo{}, hashCache: &contentModerationTestHashCache{hashes: map[string]struct{}{}},
		httpClient: server.Client(), asyncQueue: make(chan contentModerationTask, 16), keyHealth: map[string]*contentModerationKeyHealth{},
	}
	return svc, &calls
}

func checkAndPersist(t *testing.T, svc *ContentModerationService, text string) *ContentModerationLog {
	t.Helper()
	_, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 5, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody(text)})
	require.NoError(t, err)
	task := <-svc.asyncQueue
	return task.log
}

func TestContentModerationVerdictCacheServesRepeatsWithoutCallingEngine(t *testing.T) {
	svc, calls := newCountingModerationService(t, nil, nil)
	cache, store := &memoryVerdictCache{}, &fakeModerationStatsStore{}
	svc.SetVerdictCache(cache)
	svc.SetStatsStore(store)

	first := checkAndPersist(t, svc, "hello")
	second := checkAndPersist(t, svc, "hello")

	require.Equal(t, int32(1), calls.Load())
	require.False(t, first.EngineMeta.Cached)
	require.True(t, second.EngineMeta.Cached)
	require.Equal(t, 0, *second.UpstreamLatencyMS)
	require.Equal(t, ContentModerationDailyStats{Audits: 2, UpstreamCalls: 1, CacheHits: 1}, store.snapshot())
	for _, ttl := range cache.ttls {
		require.Equal(t, 24*time.Hour, ttl)
	}
}

func TestContentModerationVerdictCacheDisabledWithZeroTTL(t *testing.T) {
	svc, calls := newCountingModerationService(t, nil, func(cfg *ContentModerationConfig) { cfg.VerdictCacheTTLMinutes = 0 })
	svc.SetVerdictCache(&memoryVerdictCache{})
	checkAndPersist(t, svc, "hello")
	checkAndPersist(t, svc, "hello")
	require.Equal(t, int32(2), calls.Load())
}

func TestContentModerationVerdictCacheNeverStoresFailures(t *testing.T) {
	svc, calls := newCountingModerationService(t, []int{http.StatusBadRequest}, nil)
	cache := &memoryVerdictCache{}
	svc.SetVerdictCache(cache)
	// engine errors are written straight to the repository, not through the async queue
	_, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 5, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("hello")})
	require.NoError(t, err)
	logs := svc.repo.(*contentModerationTestRepo).snapshotLogs()
	require.Len(t, logs, 1)
	require.NotEmpty(t, logs[0].Error)
	require.Empty(t, cache.values)
	ok := checkAndPersist(t, svc, "hello")
	require.Empty(t, ok.Error)
	require.Equal(t, int32(2), calls.Load())
}

func TestContentModerationVerdictCacheKeyChangesWithModelAndRules(t *testing.T) {
	cfg := defaultContentModerationConfig()
	base := contentModerationVerdictCacheKey(cfg, "h")
	cfg.Model = "omni-moderation-2"
	require.NotEqual(t, base, contentModerationVerdictCacheKey(cfg, "h"))
	ts := newTypeSafeTestConfig("https://typesafe.example")
	require.NotEqual(t, contentModerationVerdictCacheKey(ts, "h"), base)
	require.Len(t, base, 64)
}
```

- [ ] **Step 2: 写 Redis 失败测试**

```go
package repository

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestContentModerationVerdictCacheRoundTrip(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	defer func() { _ = client.Close() }()
	cache := NewContentModerationVerdictCache(client)
	ctx := context.Background()

	_, ok, err := cache.Get(ctx, "k1")
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, cache.Set(ctx, "k1", map[string]float64{"sexual": 0.12}, time.Hour))
	scores, ok, err := cache.Get(ctx, "k1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 0.12, scores["sexual"])
	require.Equal(t, time.Hour, server.TTL("content_moderation:verdict:k1"))
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd backend && go test ./internal/service -run TestContentModerationVerdictCache -count=1; go test ./internal/repository -run TestContentModerationVerdictCache -count=1`
Expected: 编译失败

- [ ] **Step 4: 实现服务层缓存**

`backend/internal/service/content_moderation_verdict_cache.go`：

```go
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"time"
)

const (
	defaultContentModerationVerdictCacheTTLMinutes = 1440
	maxContentModerationVerdictCacheTTLMinutes     = 10080
	contentModerationVerdictCacheTimeout           = 100 * time.Millisecond
	openAIModerationRulesVersion                   = "openai-moderation"
)

type ContentModerationVerdictCache interface {
	Get(ctx context.Context, key string) (map[string]float64, bool, error)
	Set(ctx context.Context, key string, scores map[string]float64, ttl time.Duration) error
}

func (s *ContentModerationService) SetVerdictCache(c ContentModerationVerdictCache) {
	if s != nil {
		s.verdictCache = c
	}
}

func contentModerationRulesVersion(cfg *ContentModerationConfig) string {
	if cfg.Engine == ContentModerationEngineTypeSafe {
		return TypeSafeModerationRulesVersion
	}
	return openAIModerationRulesVersion
}

// contentModerationVerdictCacheKey binds a verdict to engine, model and rules so that
// switching any of them never reuses stale scores.
func contentModerationVerdictCacheKey(cfg *ContentModerationConfig, inputHash string) string {
	sum := sha256.Sum256([]byte(cfg.Engine + "\x00" + cfg.Model + "\x00" + contentModerationRulesVersion(cfg) + "\x00" + inputHash))
	return hex.EncodeToString(sum[:])
}

func (s *ContentModerationService) verdictCacheEnabled(cfg *ContentModerationConfig) bool {
	return s != nil && s.verdictCache != nil && cfg != nil && cfg.VerdictCacheTTLMinutes > 0
}

func (s *ContentModerationService) lookupVerdict(cfg *ContentModerationConfig, inputHash string) (*moderationAPIResult, bool) {
	if !s.verdictCacheEnabled(cfg) {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), contentModerationVerdictCacheTimeout)
	defer cancel()
	scores, ok, err := s.verdictCache.Get(ctx, contentModerationVerdictCacheKey(cfg, inputHash))
	if err != nil {
		slog.Warn("content_moderation.verdict_cache_get_failed", "error", err)
		return nil, false
	}
	if !ok || len(scores) == 0 {
		return nil, false
	}
	meta := &ContentModerationEngineMeta{Engine: cfg.Engine, Model: cfg.Model, Cached: true}
	if cfg.Engine == ContentModerationEngineTypeSafe {
		meta.RulesVersion = TypeSafeModerationRulesVersion
	}
	return &moderationAPIResult{CategoryScores: scores, EngineMeta: meta}, true
}

func (s *ContentModerationService) storeVerdict(cfg *ContentModerationConfig, inputHash string, result *moderationAPIResult) {
	if !s.verdictCacheEnabled(cfg) || result == nil || len(result.CategoryScores) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), contentModerationVerdictCacheTimeout)
	defer cancel()
	ttl := time.Duration(cfg.VerdictCacheTTLMinutes) * time.Minute
	if err := s.verdictCache.Set(ctx, contentModerationVerdictCacheKey(cfg, inputHash), result.CategoryScores, ttl); err != nil {
		slog.Warn("content_moderation.verdict_cache_set_failed", "error", err)
	}
}
```

- [ ] **Step 5: 配置字段与 `checkSync` 接入**

`content_moderation.go`：

1. 按 Task 10 Step 3 的 8 层清单加 `VerdictCacheTTLMinutes int \`json:"verdict_cache_ttl_minutes"\``（入参是 `*int`）。默认值 `VerdictCacheTTLMinutes: defaultContentModerationVerdictCacheTTLMinutes,`；`normalize` 加：
   ```go
   	if cfg.VerdictCacheTTLMinutes < 0 {
   		cfg.VerdictCacheTTLMinutes = 0
   	}
   	if cfg.VerdictCacheTTLMinutes > maxContentModerationVerdictCacheTTLMinutes {
   		cfg.VerdictCacheTTLMinutes = maxContentModerationVerdictCacheTTLMinutes
   	}
   ```
   （旧配置 JSON 没有这个字段时，反序列化会保留默认的 1440。）handler DTO 与映射同步加 `VerdictCacheTTLMinutes`。
2. 结构体加字段 `verdictCache ContentModerationVerdictCache`。
3. `checkSync` 中把
   ```go
   	start := time.Now()
   	result, err := s.callModeration(ctx, cfg, content.ModerationInput(), trackPreBlock)
   	latency := int(time.Since(start).Milliseconds())
   ```
   改成
   ```go
   	start := time.Now()
   	result, cached := s.lookupVerdict(cfg, hashText)
   	var err error
   	if !cached {
   		result, err = s.callModeration(ctx, cfg, content.ModerationInput(), trackPreBlock)
   		if err == nil {
   			s.storeVerdict(cfg, hashText, result)
   		}
   	}
   	latency := int(time.Since(start).Milliseconds())
   	if cached {
   		latency = 0
   	}
   ```
4. Task 2 加的统计块中，`delta := ContentModerationDailyStats{Audits: 1, UpstreamCalls: 1}` 之后加：
   ```go
   	if cached {
   		delta.UpstreamCalls, delta.CacheHits = 0, 1
   	}
   ```

- [ ] **Step 6: 实现 Redis 缓存**

`backend/internal/repository/content_moderation_verdict_cache.go`：

```go
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const contentModerationVerdictKeyPrefix = "content_moderation:verdict:"

type contentModerationVerdictCache struct {
	rdb *redis.Client
}

func NewContentModerationVerdictCache(rdb *redis.Client) service.ContentModerationVerdictCache {
	return &contentModerationVerdictCache{rdb: rdb}
}

func (c *contentModerationVerdictCache) Get(ctx context.Context, key string) (map[string]float64, bool, error) {
	if c == nil || c.rdb == nil {
		return nil, false, nil
	}
	raw, err := c.rdb.Get(ctx, contentModerationVerdictKeyPrefix+key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var scores map[string]float64
	if err := json.Unmarshal(raw, &scores); err != nil {
		return nil, false, err
	}
	return scores, true, nil
}

func (c *contentModerationVerdictCache) Set(ctx context.Context, key string, scores map[string]float64, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return nil
	}
	raw, err := json.Marshal(scores)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, contentModerationVerdictKeyPrefix+key, raw, ttl).Err()
}
```

- [ ] **Step 7: 接线**

`ProvideContentModerationService` 在 `statsStore` 后加参数 `verdictCache ContentModerationVerdictCache`，函数体加 `svc.SetVerdictCache(verdictCache)`；`repository/wire.go` 加 `NewContentModerationVerdictCache,`。

Run: `cd backend && go generate ./cmd/server && go build ./...`

- [ ] **Step 8: 运行测试确认通过**

Run: `cd backend && go test ./internal/service -run 'TestContentModeration|TestRiskControl' -count=1 && go test ./internal/repository -run 'TestContentModeration' -count=1 && go test ./internal/handler/admin -count=1`
Expected: PASS

- [ ] **Step 9: 提交**

```bash
git add backend/internal/service/content_moderation_verdict_cache.go backend/internal/service/content_moderation_verdict_cache_test.go backend/internal/service/content_moderation.go backend/internal/handler/admin/content_moderation_handler.go backend/internal/repository/content_moderation_verdict_cache.go backend/internal/repository/content_moderation_verdict_cache_test.go backend/internal/service/wire.go backend/internal/repository/wire.go backend/cmd/server/wire_gen.go
git commit -m "feat(moderation): 审计结果缓存 24 小时，相同内容不重复调用引擎"
```

---

### Task 12: 审计后移（本次不做，改为上线后观察项）

决策 Q16 = C：审计调用点有 18 处以上，`checkSecurityAudit` 同时承载上游 Prompt Audit，且有静态顺序测试 `TestPromptAuditGatePrecedesAccountBillingAndUpstreamSideEffects` 约束"审计先于扣费/选号"。Task 11 的结果缓存已覆盖客户端原样重试这部分浪费。上线一周后按日报的"审计次数 / 实际调用 / 缓存命中"判断；若失败请求仍明显消耗，再单独为 `/v1/messages` 出 change。本任务无代码改动。

---

### Task 13: 每日审计日报

**Files:**
- Create: `backend/internal/service/content_moderation_daily_report.go`
- Modify: `backend/internal/repository/content_moderation_repo.go`（新增 `NewContentModerationReportRepository` 与 `TopFlaggedUsers`）
- Modify: `backend/internal/service/wire.go`（新增 `ProvideContentModerationDailyReportService`）、`backend/internal/repository/wire.go`（加 `NewContentModerationReportRepository`）
- Modify: `backend/cmd/server/wire.go`（`provideCleanup` 增加 `moderationDailyReport *service.ContentModerationDailyReportService` 参数与 Stop 步骤）、`backend/cmd/server/wire_gen_test.go`（positional 调用补 `nil`）
- Regenerate: `backend/cmd/server/wire_gen.go`
- Test: `backend/internal/service/content_moderation_daily_report_test.go`（新）、`backend/internal/repository/content_moderation_report_repo_test.go`（新）

**Interfaces:**
- Consumes: Task 2 的 `ContentModerationStatsStore`、`ContentModerationReportLocation`、`fakeModerationStatsStore`；Task 6 的 `PushNotificationStore`、`fakePushStore`；Task 7 的 `newEnabledPushService`、`pushMessage`、`PushCategoryReport`；Task 8 的 `drainPushQueue`。
- Produces:
  - `type ContentModerationUserHitCount struct { UserID int64; UserEmail string; Hits, Blocks int64 }`
  - `type ContentModerationReportRepository interface { TopFlaggedUsers(ctx context.Context, from, to time.Time, limit int) ([]ContentModerationUserHitCount, error) }`
  - `type ContentModerationDailyReportService struct`；`func NewContentModerationDailyReportService(push *PushNotificationService, stats ContentModerationStatsStore, reports ContentModerationReportRepository, pushStore PushNotificationStore) *ContentModerationDailyReportService`；方法 `Start()`、`Stop()`、`tick(ctx context.Context) bool`
  - `func buildContentModerationDailyReport(day time.Time, stats ContentModerationDailyStats, top []ContentModerationUserHitCount, sent map[string]int64) pushMessage`
  - `repository.NewContentModerationReportRepository(db *sql.DB) service.ContentModerationReportRepository`

- [ ] **Step 1: 写服务层失败测试**

```go
package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeModerationReportRepo struct {
	top      []ContentModerationUserHitCount
	from, to time.Time
}

func (f *fakeModerationReportRepo) TopFlaggedUsers(_ context.Context, from, to time.Time, _ int) ([]ContentModerationUserHitCount, error) {
	f.from, f.to = from, to
	return f.top, nil
}

func TestContentModerationDailyReportSendsOncePerDayAfterHour(t *testing.T) {
	push, sender, pushStore := newEnabledPushService(t, nil)
	stats := &fakeModerationStatsStore{total: ContentModerationDailyStats{Audits: 1000, UpstreamCalls: 400, CacheHits: 600, InputTokens: 2_000_000, Flagged: 12, Blocked: 9, ObserveFlagged: 3, HashBlocks: 4}}
	reports := &fakeModerationReportRepo{top: []ContentModerationUserHitCount{{UserID: 16, UserEmail: "big@example.com", Hits: 8, Blocks: 0}}}
	svc := NewContentModerationDailyReportService(push, stats, reports, pushStore)
	loc := ContentModerationReportLocation()
	ctx := context.Background()

	svc.now = func() time.Time { return time.Date(2026, 11, 15, 8, 59, 0, 0, loc) }
	require.False(t, svc.tick(ctx), "before 09:00")

	svc.now = func() time.Time { return time.Date(2026, 11, 15, 9, 1, 0, 0, loc) }
	require.True(t, svc.tick(ctx))
	require.False(t, svc.tick(ctx), "only once per day")
	drainPushQueue(ctx, push)

	sent := sender.snapshot()
	require.Len(t, sent, 1)
	require.Equal(t, "【日报】内容审计 11-14", sent[0].Title)
	require.Contains(t, sent[0].Desp, "1000")
	require.Contains(t, sent[0].Desp, "60.0%")
	require.Contains(t, sent[0].Desp, "$0.08")
	require.Contains(t, sent[0].Desp, "big@example.com")
	require.Equal(t, time.Date(2026, 11, 14, 0, 0, 0, 0, loc), reports.from)
	require.Equal(t, time.Date(2026, 11, 15, 0, 0, 0, 0, loc), reports.to)
}

func TestContentModerationDailyReportSkipsWhenDisabled(t *testing.T) {
	push, sender, pushStore := newEnabledPushService(t, func(c *PushNotificationConfig) { c.DailyReportEnabled = false })
	svc := NewContentModerationDailyReportService(push, &fakeModerationStatsStore{}, &fakeModerationReportRepo{}, pushStore)
	svc.now = func() time.Time { return time.Date(2026, 11, 15, 10, 0, 0, 0, ContentModerationReportLocation()) }
	require.False(t, svc.tick(context.Background()))
	require.Empty(t, sender.snapshot())
}

func TestBuildContentModerationDailyReportWithoutHits(t *testing.T) {
	msg := buildContentModerationDailyReport(time.Date(2026, 11, 14, 0, 0, 0, 0, ContentModerationReportLocation()), ContentModerationDailyStats{}, nil, nil)
	require.Equal(t, PushCategoryReport, msg.category)
	require.Contains(t, msg.desp, "无")
}
```

- [ ] **Step 2: 写 repository 失败测试**

```go
package repository

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestContentModerationTopFlaggedUsers(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	from := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)
	mock.ExpectQuery(`SELECT COALESCE\(user_id, 0\), user_email`).WithArgs(from, to, 5).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "user_email", "hits", "blocks"}).AddRow(16, "big@example.com", 8, 0).AddRow(1, "a@example.com", 3, 3))

	got, err := NewContentModerationReportRepository(db).TopFlaggedUsers(context.Background(), from, to, 5)

	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, int64(16), got[0].UserID)
	require.Equal(t, int64(8), got[0].Hits)
	require.Equal(t, int64(3), got[1].Blocks)
	require.NoError(t, mock.ExpectationsWereMet())
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd backend && go test ./internal/service -run 'TestContentModerationDailyReport|TestBuildContentModerationDailyReport' -count=1; go test ./internal/repository -run TestContentModerationTopFlaggedUsers -count=1`
Expected: 编译失败

- [ ] **Step 4: 实现日报服务**

`backend/internal/service/content_moderation_daily_report.go`：

```go
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
```

- [ ] **Step 5: 实现 repository 查询**

追加到 `content_moderation_repo.go`：

```go
func NewContentModerationReportRepository(db *sql.DB) service.ContentModerationReportRepository {
	return &contentModerationRepository{db: db}
}

func (r *contentModerationRepository) TopFlaggedUsers(ctx context.Context, from, to time.Time, limit int) ([]service.ContentModerationUserHitCount, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT COALESCE(user_id, 0), user_email, COUNT(*) AS hits,
       COUNT(*) FILTER (WHERE action IN ('block', 'hash_block', 'keyword_block')) AS blocks
FROM content_moderation_logs
WHERE flagged AND action <> 'cyber_policy' AND created_at >= $1 AND created_at < $2
GROUP BY COALESCE(user_id, 0), user_email
ORDER BY hits DESC, 1 ASC
LIMIT $3`, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("query top flagged users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []service.ContentModerationUserHitCount
	for rows.Next() {
		var item service.ContentModerationUserHitCount
		if err := rows.Scan(&item.UserID, &item.UserEmail, &item.Hits, &item.Blocks); err != nil {
			return nil, fmt.Errorf("scan top flagged users: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
```

- [ ] **Step 6: 接线与关停**

`service/wire.go`：

```go
// ProvideContentModerationDailyReportService creates and starts the daily push report.
func ProvideContentModerationDailyReportService(
	push *PushNotificationService,
	stats ContentModerationStatsStore,
	reports ContentModerationReportRepository,
	pushStore PushNotificationStore,
) *ContentModerationDailyReportService {
	svc := NewContentModerationDailyReportService(push, stats, reports, pushStore)
	svc.Start()
	return svc
}
```

ProviderSet 加 `ProvideContentModerationDailyReportService,`；`repository/wire.go` 加 `NewContentModerationReportRepository,`；`provideCleanup` 参数末尾加 `moderationDailyReport *service.ContentModerationDailyReportService,`，`parallelSteps` 加：

```go
			{"ContentModerationDailyReportService", func() error {
				if moderationDailyReport != nil {
					moderationDailyReport.Stop()
				}
				return nil
			}},
```

`wire_gen_test.go` 的 `provideCleanup(...)` 末尾再补一个 `nil, // moderationDailyReport`。

Run: `cd backend && go generate ./cmd/server && go build ./...`

- [ ] **Step 7: 运行测试确认通过**

Run: `cd backend && go test ./internal/service -run 'TestContentModerationDailyReport|TestBuildContentModerationDailyReport|TestPush' -count=1 && go test ./internal/repository -run TestContentModeration -count=1 && go test ./cmd/server -count=1`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add backend/internal/service/content_moderation_daily_report.go backend/internal/service/content_moderation_daily_report_test.go backend/internal/repository/content_moderation_repo.go backend/internal/repository/content_moderation_report_repo_test.go backend/internal/service/wire.go backend/internal/repository/wire.go backend/cmd/server/wire.go backend/cmd/server/wire_gen.go backend/cmd/server/wire_gen_test.go
git commit -m "feat(push): 每日 09:00 推送前一天的内容审计日报"
```

---

### Task 14: 前端：观察名单、缓存时长、日志搜索预填

**Files:**
- Modify: `frontend/src/api/admin/riskControl.ts`（`ModerationEngineMeta`、`ContentModerationConfig` 约 :19-54、`UpdateContentModerationConfig` 约 :103-139）
- Modify: `frontend/src/views/admin/RiskControlView.vue`（scope tab :721、runtime tab :823、`configForm` :1259、`applyConfig` :1782、`saveConfig` :1878、`filters` :1353、`onMounted` :2459、imports :1150-1180）
- Modify: `frontend/src/i18n/locales/zh/admin/channels.ts`、`frontend/src/i18n/locales/en/admin/channels.ts`（`admin.riskControl.*`）
- Modify: `frontend/src/i18n/locales/zh/admin/settings.ts:88`、`frontend/src/i18n/locales/en/admin/settings.ts`（`riskControlUserAllowlistHint`）
- Test: `frontend/src/views/admin/__tests__/RiskControlView.spec.ts`

**Interfaces:**
- Consumes：后端字段 `observe_user_ids: number[]`、`verdict_cache_ttl_minutes: number`、`engine_meta.input_tokens`、`engine_meta.cached`（Task 1、10、11）。
- Produces：i18n key `admin.riskControl.observeUsers`、`observeUsersHint`、`verdictCacheTTL`、`verdictCacheTTLHint`；`data-test="observe-users-selector"`、`data-test="verdict-cache-ttl-input"`。

- [ ] **Step 1: 写失败的测试**

在 `RiskControlView.spec.ts` 的 `baseConfig()` 返回值里加 `observe_user_ids: [16]`、`verdict_cache_ttl_minutes: 1440`，并新增：

```ts
  it('saves observe users and verdict cache ttl', async () => {
    const ObserveSelectorStub = defineComponent({
      props: { modelValue: { type: Array, default: () => [] } },
      emits: ['update:modelValue'],
      setup(props) {
        return () => h('div', { 'data-test': 'observe-users-selector', 'data-value': JSON.stringify(props.modelValue) })
      },
    })
    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub, BaseDialog: BaseDialogStub, Icon: true, Select: true, Toggle: true, Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub, ProxySelector: true, OpenAIFastPolicyUserSelector: ObserveSelectorStub,
        },
      },
    })
    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.scope').trigger('click')
    const selector = wrapper.findComponent(ObserveSelectorStub)
    expect(selector.attributes('data-value')).toBe('[16]')
    selector.vm.$emit('update:modelValue', [16, 22])
    await findButtonByText(wrapper, 'admin.riskControl.tabs.runtime').trigger('click')
    await wrapper.get('[data-test="verdict-cache-ttl-input"]').setValue('60')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({ observe_user_ids: [16, 22], verdict_cache_ttl_minutes: 60 }))
    expect(showError).not.toHaveBeenCalled()
  })
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && pnpm vitest run src/views/admin/__tests__/RiskControlView.spec.ts`
Expected: FAIL（找不到 selector / input；TS 类型缺字段）

- [ ] **Step 3: API 类型**

`riskControl.ts`：

```ts
export interface ModerationEngineMeta {
  engine: ModerationEngine
  model: string
  rules_version: string
  skipped_images: number
  input_tokens?: number
  cached?: boolean
}
```

`ContentModerationConfig` 在 `group_ids: number[]` 后加 `observe_user_ids: number[]` 和 `verdict_cache_ttl_minutes: number`；`UpdateContentModerationConfig` 在 `group_ids?: number[]` 后加 `observe_user_ids?: number[]` 和 `verdict_cache_ttl_minutes?: number`。

- [ ] **Step 4: 视图**

`RiskControlView.vue`：

1. import：
   ```ts
   import { useRoute } from 'vue-router'
   import OpenAIFastPolicyUserSelector from '@/views/admin/settings/OpenAIFastPolicyUserSelector.vue'
   ```
2. `configForm` 加 `observe_user_ids: [] as number[],` 和 `verdict_cache_ttl_minutes: 1440,`。
3. `applyConfig` 加：
   ```ts
   configForm.observe_user_ids = Array.isArray(config.observe_user_ids) ? [...config.observe_user_ids] : []
   configForm.verdict_cache_ttl_minutes = typeof config.verdict_cache_ttl_minutes === 'number' ? config.verdict_cache_ttl_minutes : 1440
   ```
4. `saveConfig` 的 payload 加：
   ```ts
   observe_user_ids: [...configForm.observe_user_ids],
   verdict_cache_ttl_minutes: Math.max(0, Math.min(10080, Number(configForm.verdict_cache_ttl_minutes) || 0)),
   ```
5. scope tab（`activeSettingsTab === 'scope'` 的容器）末尾加：
   ```vue
   <div class="space-y-2 rounded-lg border border-gray-100 p-4 dark:border-dark-700">
     <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.observeUsers') }}</p>
     <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.observeUsersHint') }}</p>
     <OpenAIFastPolicyUserSelector v-model="configForm.observe_user_ids" data-test="observe-users-selector" />
   </div>
   ```
6. runtime tab（`activeSettingsTab === 'runtime'` 的 grid）里，`queue_size` 输入框之后加：
   ```vue
   <div>
     <label class="input-label">{{ t('admin.riskControl.verdictCacheTTL') }}</label>
     <input v-model.number="configForm.verdict_cache_ttl_minutes" type="number" min="0" max="10080" class="input" data-test="verdict-cache-ttl-input" />
     <p class="input-hint">{{ t('admin.riskControl.verdictCacheTTLHint') }}</p>
   </div>
   ```
7. 日志搜索预填：在 `const filters = reactive({...})` 之后加：
   ```ts
   const route = useRoute()
   const initialSearch = route?.query?.search
   if (typeof initialSearch === 'string' && initialSearch.trim() !== '') {
     filters.search = initialSearch.trim()
   }
   ```

- [ ] **Step 5: i18n**

`zh/admin/channels.ts` 的 `riskControl` 块（`groupScopeHint` 附近）加：

```ts
      observeUsers: '审计观察名单',
      observeUsersHint: '名单内用户照常审计、命中会推送，但不会被拦截、不发违规邮件、不计入封号次数。用于新用户或大客户开拦前的观察期，清空即开始拦截。',
      verdictCacheTTL: '审计结果缓存（分钟）',
      verdictCacheTTLHint: '相同内容在缓存时间内直接复用上次结果，不再调用审计引擎。0 表示关闭，最长 10080（7 天）。',
```

`en/admin/channels.ts` 同位置加：

```ts
      observeUsers: 'Audit observe list',
      observeUsersHint: 'Users on this list are audited and notified on hits, but never blocked, emailed or counted toward bans. Use it for an observation period before enforcement; clearing the list starts blocking.',
      verdictCacheTTL: 'Verdict cache (minutes)',
      verdictCacheTTLHint: 'Identical content reuses the previous verdict within this window without calling the engine. 0 disables; max 10080 (7 days).',
```

`zh/admin/settings.ts` 的 `riskControlUserAllowlistHint` 改为：

```ts
          riskControlUserAllowlistHint: '输入任意邮箱关键词进行模糊搜索。白名单只影响 OpenAI 上游 cyber_policy：不会触发封号或本地会话屏蔽，但仍然无法突破上游拦截。内容审计不再豁免白名单用户，如需观察期请使用风控中心的"审计观察名单"。',
```

`en/admin/settings.ts` 对应改为：

```ts
          riskControlUserAllowlistHint: 'Search by any email keyword. The allowlist only affects OpenAI upstream cyber_policy: no bans or local session blocks, but upstream rejections still apply. Content moderation no longer exempts these users; use the audit observe list in Risk Control for an observation period.',
```

- [ ] **Step 6: 运行测试确认通过**

Run: `cd frontend && pnpm vitest run src/views/admin/__tests__/RiskControlView.spec.ts src/i18n/__tests__/localeKeyCompleteness.spec.ts && pnpm vue-tsc --noEmit`
Expected: PASS，类型检查无错误

- [ ] **Step 7: 提交**

```bash
git add frontend/src/api/admin/riskControl.ts frontend/src/views/admin/RiskControlView.vue frontend/src/views/admin/__tests__/RiskControlView.spec.ts frontend/src/i18n/locales/zh/admin/channels.ts frontend/src/i18n/locales/en/admin/channels.ts frontend/src/i18n/locales/zh/admin/settings.ts frontend/src/i18n/locales/en/admin/settings.ts
git commit -m "feat(risk-control): 前端支持审计观察名单与结果缓存时长"
```

---

### Task 15: 前端：微信推送设置

**Files:**
- Modify: `frontend/src/api/admin/riskControl.ts`（推送类型与 3 个函数，并加入 `riskControlAPI`）
- Create: `frontend/src/views/admin/riskControl/RiskControlPushDialog.vue`
- Modify: `frontend/src/views/admin/RiskControlView.vue`（顶部按钮 :14-23；挂载对话框）
- Modify: `frontend/src/i18n/locales/zh/admin/channels.ts`、`frontend/src/i18n/locales/en/admin/channels.ts`（`admin.riskControl.push.*`）
- Test: `frontend/src/views/admin/riskControl/__tests__/RiskControlPushDialog.spec.ts`（新）

**Interfaces:**
- Consumes：Task 6 的接口 `GET/PUT /admin/risk-control/push/config`、`POST /admin/risk-control/push/test`。
- Produces：`PushNotificationConfig`、`UpdatePushNotificationConfig` 类型；`getPushConfig()`、`updatePushConfig(payload)`、`sendPushTest()`；组件 `RiskControlPushDialog`（props `show: boolean`，emit `close`）。

- [ ] **Step 1: 写失败的测试**

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

import RiskControlPushDialog from '../RiskControlPushDialog.vue'

const { getPushConfig, updatePushConfig, sendPushTest, showError, showSuccess } = vi.hoisted(() => ({
  getPushConfig: vi.fn(),
  updatePushConfig: vi.fn(),
  sendPushTest: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({ adminAPI: { riskControl: { getPushConfig, updatePushConfig, sendPushTest } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const BaseDialogStub = defineComponent({
  props: { show: Boolean, title: String },
  setup(props, { slots }) {
    return () => (props.show ? h('div', [slots.default?.(), slots.footer?.()]) : null)
  },
})

const config = () => ({
  enabled: true, send_key_configured: true, send_key_masked: '********F123',
  hit_daily_cap: 300, hit_window_minutes: 30, daily_report_enabled: true, daily_report_hour: 9, ops_critical_enabled: true,
})

function mountDialog() {
  return mount(RiskControlPushDialog, { props: { show: true }, global: { stubs: { BaseDialog: BaseDialogStub, Toggle: true } } })
}

describe('RiskControlPushDialog', () => {
  beforeEach(() => {
    getPushConfig.mockReset().mockResolvedValue(config())
    updatePushConfig.mockReset().mockImplementation(async (p) => ({ ...config(), ...p }))
    sendPushTest.mockReset().mockResolvedValue({ message: 'ok' })
    showError.mockReset()
    showSuccess.mockReset()
  })

  it('shows the masked key and never sends it back when unchanged', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    expect(wrapper.get('[data-test="push-send-key"]').attributes('placeholder')).toBe('********F123')
    await wrapper.get('[data-test="push-hit-cap"]').setValue('200')
    await wrapper.get('[data-test="push-save"]').trigger('click')
    await flushPromises()
    const payload = updatePushConfig.mock.calls[0][0]
    expect(payload.hit_daily_cap).toBe(200)
    expect(payload).not.toHaveProperty('send_key')
    expect(showSuccess).toHaveBeenCalled()
  })

  it('sends a new key and triggers a test push', async () => {
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-test="push-send-key"]').setValue('SCT999newKEY')
    await wrapper.get('[data-test="push-save"]').trigger('click')
    await flushPromises()
    expect(updatePushConfig.mock.calls[0][0].send_key).toBe('SCT999newKEY')
    await wrapper.get('[data-test="push-test"]').trigger('click')
    await flushPromises()
    expect(sendPushTest).toHaveBeenCalledTimes(1)
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && pnpm vitest run src/views/admin/riskControl/__tests__/RiskControlPushDialog.spec.ts`
Expected: FAIL（组件不存在）

- [ ] **Step 3: API**

`riskControl.ts` 追加：

```ts
export interface PushNotificationConfig {
  enabled: boolean
  send_key_configured: boolean
  send_key_masked: string
  hit_daily_cap: number
  hit_window_minutes: number
  daily_report_enabled: boolean
  daily_report_hour: number
  ops_critical_enabled: boolean
}

export interface UpdatePushNotificationConfig {
  enabled?: boolean
  send_key?: string
  clear_send_key?: boolean
  hit_daily_cap?: number
  hit_window_minutes?: number
  daily_report_enabled?: boolean
  daily_report_hour?: number
  ops_critical_enabled?: boolean
}

export async function getPushConfig(): Promise<PushNotificationConfig> {
  const { data } = await apiClient.get<PushNotificationConfig>('/admin/risk-control/push/config')
  return data
}

export async function updatePushConfig(payload: UpdatePushNotificationConfig): Promise<PushNotificationConfig> {
  const { data } = await apiClient.put<PushNotificationConfig>('/admin/risk-control/push/config', payload)
  return data
}

export async function sendPushTest(): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>('/admin/risk-control/push/test')
  return data
}
```

`riskControlAPI` 对象加 `getPushConfig, updatePushConfig, sendPushTest,`。

- [ ] **Step 4: 对话框组件**

`frontend/src/views/admin/riskControl/RiskControlPushDialog.vue`：

```vue
<template>
  <BaseDialog :show="show" :title="t('admin.riskControl.push.title')" width="normal" @close="emit('close')">
    <div class="space-y-5">
      <div class="flex items-center justify-between">
        <div>
          <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.riskControl.push.enabled') }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.push.enabledHint') }}</p>
        </div>
        <Toggle v-model="form.enabled" />
      </div>
      <div>
        <label class="input-label">{{ t('admin.riskControl.push.sendKey') }}</label>
        <input v-model.trim="form.send_key" type="password" autocomplete="off" class="input" data-test="push-send-key"
          :placeholder="configured ? maskedKey : 'SCT...'" />
        <p class="input-hint">{{ configured ? t('admin.riskControl.push.sendKeyConfigured', { masked: maskedKey }) : t('admin.riskControl.push.sendKeyHint') }}</p>
      </div>
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <label class="input-label">{{ t('admin.riskControl.push.hitDailyCap') }}</label>
          <input v-model.number="form.hit_daily_cap" type="number" min="1" max="1000" class="input" data-test="push-hit-cap" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.riskControl.push.hitWindowMinutes') }}</label>
          <input v-model.number="form.hit_window_minutes" type="number" min="5" max="240" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.riskControl.push.dailyReportHour') }}</label>
          <input v-model.number="form.daily_report_hour" type="number" min="0" max="23" class="input" />
        </div>
      </div>
      <div class="flex items-center justify-between">
        <p class="text-sm text-gray-900 dark:text-white">{{ t('admin.riskControl.push.dailyReportEnabled') }}</p>
        <Toggle v-model="form.daily_report_enabled" />
      </div>
      <div class="flex items-center justify-between">
        <p class="text-sm text-gray-900 dark:text-white">{{ t('admin.riskControl.push.opsCriticalEnabled') }}</p>
        <Toggle v-model="form.ops_critical_enabled" />
      </div>
    </div>
    <template #footer>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-secondary" :disabled="testing || !configured" data-test="push-test" @click="sendTest">
          {{ t('admin.riskControl.push.sendTest') }}
        </button>
        <button type="button" class="btn btn-primary" :disabled="saving" data-test="push-save" @click="save">
          {{ t('admin.riskControl.push.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import { adminAPI } from '@/api/admin'
import type { PushNotificationConfig, UpdatePushNotificationConfig } from '@/api/admin/riskControl'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const appStore = useAppStore()

const configured = ref(false)
const maskedKey = ref('')
const saving = ref(false)
const testing = ref(false)
const form = reactive({
  enabled: false, send_key: '', hit_daily_cap: 300, hit_window_minutes: 30,
  daily_report_enabled: true, daily_report_hour: 9, ops_critical_enabled: true,
})

function apply(cfg: PushNotificationConfig) {
  configured.value = cfg.send_key_configured
  maskedKey.value = cfg.send_key_masked
  Object.assign(form, {
    enabled: cfg.enabled, send_key: '', hit_daily_cap: cfg.hit_daily_cap, hit_window_minutes: cfg.hit_window_minutes,
    daily_report_enabled: cfg.daily_report_enabled, daily_report_hour: cfg.daily_report_hour, ops_critical_enabled: cfg.ops_critical_enabled,
  })
}

async function load() {
  try {
    apply(await adminAPI.riskControl.getPushConfig())
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.push.loadFailed')))
  }
}

async function save() {
  saving.value = true
  try {
    const payload: UpdatePushNotificationConfig = {
      enabled: form.enabled, hit_daily_cap: Number(form.hit_daily_cap), hit_window_minutes: Number(form.hit_window_minutes),
      daily_report_enabled: form.daily_report_enabled, daily_report_hour: Number(form.daily_report_hour), ops_critical_enabled: form.ops_critical_enabled,
    }
    if (form.send_key !== '') {
      payload.send_key = form.send_key
    }
    apply(await adminAPI.riskControl.updatePushConfig(payload))
    appStore.showSuccess(t('admin.riskControl.push.saved'))
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.push.saveFailed')))
  } finally {
    saving.value = false
  }
}

async function sendTest() {
  testing.value = true
  try {
    await adminAPI.riskControl.sendPushTest()
    appStore.showSuccess(t('admin.riskControl.push.testSent'))
  } catch (err) {
    appStore.showError(extractApiErrorMessage(err, t('admin.riskControl.push.testFailed')))
  } finally {
    testing.value = false
  }
}

watch(() => props.show, (visible) => { if (visible) void load() }, { immediate: true })
</script>
```

- [ ] **Step 5: 挂到风控页**

`RiskControlView.vue`：import `RiskControlPushDialog from '@/views/admin/riskControl/RiskControlPushDialog.vue'`；加 `const pushDialogOpen = ref(false)`；顶部按钮区在"设置"按钮之前加：

```vue
<button type="button" class="btn btn-secondary inline-flex items-center gap-2" data-test="open-push-settings" @click="pushDialogOpen = true">
  <Icon name="bell" size="sm" />
  {{ t('admin.riskControl.push.open') }}
</button>
```

模板末尾（其它 `BaseDialog` 之后）加 `<RiskControlPushDialog :show="pushDialogOpen" @close="pushDialogOpen = false" />`。`RiskControlView.spec.ts` 的 stubs 加 `RiskControlPushDialog: true`（所有 mount 调用）。

- [ ] **Step 6: i18n**

`zh/admin/channels.ts` 的 `riskControl` 块加：

```ts
      push: {
        open: '微信推送',
        title: '微信推送（Server酱）',
        enabled: '启用推送',
        enabledHint: '审计命中、审计故障告警、ops P0 告警和每日日报会推送到微信。推送内容不包含违规原文。',
        sendKey: 'Server酱 SendKey',
        sendKeyHint: '在 sct.ftqq.com 获取，以 SCT 开头。',
        sendKeyConfigured: '已配置 {masked}，留空保持不变',
        hitDailyCap: '命中推送每日上限',
        hitWindowMinutes: '同一用户合并窗口（分钟）',
        dailyReportHour: '日报时间（北京时间，点）',
        dailyReportEnabled: '每日日报',
        opsCriticalEnabled: '推送 ops P0 告警',
        sendTest: '发送测试',
        save: '保存',
        saved: '推送设置已保存',
        saveFailed: '保存推送设置失败',
        loadFailed: '加载推送设置失败',
        testSent: '测试推送已发送，请查看微信',
        testFailed: '测试推送失败',
      },
```

`en/admin/channels.ts` 同结构：

```ts
      push: {
        open: 'WeChat push',
        title: 'WeChat push (ServerChan)',
        enabled: 'Enable push',
        enabledHint: 'Audit hits, audit outage alerts, ops P0 alerts and the daily report are pushed to WeChat. Pushes never include the flagged text.',
        sendKey: 'ServerChan SendKey',
        sendKeyHint: 'Get it at sct.ftqq.com; it starts with SCT.',
        sendKeyConfigured: 'Configured {masked}; leave empty to keep it',
        hitDailyCap: 'Daily cap for hit pushes',
        hitWindowMinutes: 'Per-user merge window (minutes)',
        dailyReportHour: 'Daily report hour (Beijing time)',
        dailyReportEnabled: 'Daily report',
        opsCriticalEnabled: 'Push ops P0 alerts',
        sendTest: 'Send test',
        save: 'Save',
        saved: 'Push settings saved',
        saveFailed: 'Failed to save push settings',
        loadFailed: 'Failed to load push settings',
        testSent: 'Test push sent; check WeChat',
        testFailed: 'Test push failed',
      },
```

- [ ] **Step 7: 运行测试确认通过**

Run: `cd frontend && pnpm vitest run src/views/admin/riskControl/__tests__/RiskControlPushDialog.spec.ts src/views/admin/__tests__/RiskControlView.spec.ts src/i18n/__tests__/localeKeyCompleteness.spec.ts && pnpm vue-tsc --noEmit`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add frontend/src/api/admin/riskControl.ts frontend/src/views/admin/riskControl frontend/src/views/admin/RiskControlView.vue frontend/src/views/admin/__tests__/RiskControlView.spec.ts frontend/src/i18n/locales/zh/admin/channels.ts frontend/src/i18n/locales/en/admin/channels.ts
git commit -m "feat(risk-control): 前端微信推送设置对话框"
```

---

### Task 16: 全量验证与发版准备

**Files:**
- Modify: `openspec/changes/moderation-observe-push-cost/proposal.md`（勾选验收项、记录实测数字）

- [ ] **Step 1: 后端全量**

Run: `cd backend && go test -tags=unit ./... && go test ./internal/service/... ./internal/repository/... ./internal/handler/... ./migrations/... ./cmd/server/... -count=1 && golangci-lint run ./...`
Expected: 全部 PASS，lint 无新增问题

- [ ] **Step 2: 前端全量**

Run: `cd frontend && pnpm vitest run && pnpm vue-tsc --noEmit && pnpm build`
Expected: PASS，构建成功

- [ ] **Step 3: 本地冒烟**

1. 本地启动后端（`DEV_GUIDE.md` 的本地 PG/Redis），后台打开风控中心 → 微信推送，填测试 SendKey，点"发送测试"，确认微信收到。
2. 设置对话框 → 范围 tab，确认观察名单能搜索并保存；运行 tab 确认缓存时长保存后刷新仍在。
3. 打开 `/admin/risk-control?search=<某 request_id>`，确认日志列表自动按该值过滤。
4. `redis-cli keys 'content_moderation:*'` 能看到 `stats:<日期>` 和 `verdict:*`。

- [ ] **Step 4: 发版说明草稿**

按 memory「发版更新日志必须有意义」，准备 annotated tag 正文（亮点 / 新增 / 优化 / 修复 / 升级说明）。升级说明必须写明：
- 迁移 `248a` 会把风控白名单复制进内容审计的"审计观察名单"，升级后行为不变；风控白名单此后只管 OpenAI cyber_policy；清空观察名单才会开始拦截原白名单用户。
- 审计结果缓存默认开启（24 小时），可在设置 → 运行 tab 调整或设为 0 关闭。
- 推送默认关闭，需要在风控中心 → 微信推送里填 Server酱 SendKey 并启用。

- [ ] **Step 5: 提交**

```bash
git add openspec/changes/moderation-observe-push-cost/proposal.md
git commit -m "docs(openspec): 记录内容审计观察名单与推送改造的验证结果"
```

> 上线（打 tag、us-40 备份升级、填 SendKey、充值后改生产配置、观察 3 天后清空观察名单）按 `proposal.md` 的"上线步骤"执行，第 4、6 步改生产配置前必须再次征得用户确认，不属于本计划的自动执行范围。
