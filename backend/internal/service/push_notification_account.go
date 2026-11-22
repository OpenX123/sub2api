package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// pushAccountErrorDedupTTL 同一次异常（账号 ID + 进入异常时的 updated_at）只推送一次。
	pushAccountErrorDedupTTL = 30 * 24 * time.Hour
	pushAccountErrorMaxLines = 15
	pushAccountErrorMsgLimit = 160
)

// PushAccountSource 提供处于异常状态的账号列表。
type PushAccountSource interface {
	ListAllWithFilters(ctx context.Context, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, error)
}

// accountErrorSeen 是没有 Redis 时的进程内去重兜底。
type accountErrorSeen struct {
	mu   sync.Mutex
	keys map[string]struct{}
}

func (a *accountErrorSeen) firstTime(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.keys == nil {
		a.keys = make(map[string]struct{})
	}
	if _, ok := a.keys[key]; ok {
		return false
	}
	a.keys[key] = struct{}{}
	return true
}

// SetAccountSource 接入账号数据源后，推送服务每分钟巡检一次新进入异常状态的账号。
func (s *PushNotificationService) SetAccountSource(src PushAccountSource) {
	if s != nil {
		s.accounts = src
	}
}

// checkAccountErrors 推送本轮新发现的异常账号（status=error，需要人工处理）。
// 账号转为异常的写入路径很多（刷新失败、401/403、封禁等），这里统一按结果巡检，
// 多个账号同时出问题时合并成一条消息。
func (s *PushNotificationService) checkAccountErrors(ctx context.Context) {
	if s == nil || s.accounts == nil {
		return
	}
	cfg := s.loadConfig(ctx)
	if !cfg.Enabled || !cfg.AccountAlertEnabled {
		return
	}
	accounts, err := s.accounts.ListAllWithFilters(ctx, "", "", StatusError, "", 0, "")
	if err != nil {
		slog.Warn("push_notification.account_list_failed", "error", err)
		return
	}
	fresh := make([]Account, 0)
	for _, acc := range accounts {
		if acc.ID <= 0 || acc.Status != StatusError {
			continue
		}
		key := fmt.Sprintf("push:account_error:%d:%d", acc.ID, acc.UpdatedAt.Unix())
		if s.store != nil {
			first, err := s.store.TryOnce(ctx, key, pushAccountErrorDedupTTL)
			if err != nil {
				slog.Warn("push_notification.account_dedup_failed", "account_id", acc.ID, "error", err)
				continue
			}
			if !first {
				continue
			}
		} else if !s.accountSeen.firstTime(key) {
			continue
		}
		fresh = append(fresh, acc)
	}
	if len(fresh) == 0 {
		return
	}
	s.enqueue(formatAccountErrors(fresh, s.adminPageLink(ctx, "/admin/accounts")))
}

func (s *PushNotificationService) adminPageLink(ctx context.Context, path string) string {
	if s == nil || s.settingRepo == nil {
		return ""
	}
	base, err := s.settingRepo.GetValue(ctx, SettingKeyFrontendURL)
	if err != nil || strings.TrimSpace(base) == "" {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(base), "/") + path
}

func formatAccountErrors(accounts []Account, link string) pushMessage {
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].UpdatedAt.After(accounts[j].UpdatedAt) })
	var b strings.Builder
	for i, acc := range accounts {
		if i >= pushAccountErrorMaxLines {
			fmt.Fprintf(&b, "\n…另有 %d 个账号，请到后台查看。\n", len(accounts)-pushAccountErrorMaxLines)
			break
		}
		fmt.Fprintf(&b, "- **%s**（#%d，%s/%s", acc.Name, acc.ID, acc.Platform, acc.Type)
		if groups := accountGroupNames(acc); groups != "" {
			fmt.Fprintf(&b, "，分组 %s", groups)
		}
		fmt.Fprint(&b, "）\n")
		msg := strings.TrimSpace(acc.ErrorMessage)
		if msg == "" {
			msg = "未记录错误信息"
		}
		fmt.Fprintf(&b, "  %s\n", truncateString(msg, pushAccountErrorMsgLimit))
	}
	fmt.Fprint(&b, "\n账号已停止调度，需要人工处理（重新授权、更换凭据或删除）。\n")
	if link != "" {
		fmt.Fprintf(&b, "\n[去后台处理](%s)\n", link)
	}
	title := fmt.Sprintf("【账号异常】%s", accounts[0].Name)
	if len(accounts) > 1 {
		title = fmt.Sprintf("【账号异常】%d 个账号停止调度", len(accounts))
	}
	return pushMessage{category: PushCategoryAccount, title: title, desp: b.String()}
}

func accountGroupNames(acc Account) string {
	names := make([]string, 0, len(acc.Groups))
	for _, g := range acc.Groups {
		if g != nil && strings.TrimSpace(g.Name) != "" {
			names = append(names, strings.TrimSpace(g.Name))
		}
	}
	return strings.Join(names, "、")
}
