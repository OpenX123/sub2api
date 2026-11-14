package service

import (
	"fmt"
	"strings"
)

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

func formatHitCapNotice(limit int) pushMessage {
	return pushMessage{category: PushCategoryNotice, title: "【提示】今日命中推送已达上限",
		desp: fmt.Sprintf("今天的命中推送已达到 %d 条上限，之后的命中不再实时推送，请查看后台和明天的日报。", limit)}
}

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
