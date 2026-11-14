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

func formatHitCapNotice(limit int) pushMessage {
	return pushMessage{category: PushCategoryNotice, title: "【提示】今日命中推送已达上限",
		desp: fmt.Sprintf("今天的命中推送已达到 %d 条上限，之后的命中不再实时推送，请查看后台和明天的日报。", limit)}
}
