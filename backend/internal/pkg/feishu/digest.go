package feishu

import (
	"context"
	"fmt"
	"strings"
)

type DigestCard struct {
	AnalysisCurrent, AnalysisPrevious            DigestWindowMetrics
	Title, Window, Pool, Concurrency, PoolDetail string
	RPM, Latency, Duration                       string
	Highlight, Risk, Spend, Errors               string
	RiskRows, SpendRows, ErrorRows               []string
	Examples, TrendRows, Suggestions             []string
	TokenShare, SpendShare, ErrorImpact          string
	Trend                                        []TrendPoint
}

type TrendPoint struct {
	Hour     string  `json:"hour"`
	User     string  `json:"user"`
	Millions float64 `json:"millions"`
}

type component map[string]any

func cardText(s string) component { return component{"tag": "plain_text", "content": s} }
func cardLine(s string) component { return component{"tag": "div", "text": cardText(s)} }
func metricPair(left, right string) component {
	columns := []component{}
	for _, value := range []string{left, right} {
		columns = append(columns, component{"tag": "column", "width": "weighted", "weight": 1, "elements": []component{cardLine(value)}})
	}
	return component{"tag": "column_set", "flex_mode": "bisect", "columns": columns}
}
func detailPanel(summary string, rows []string) component {
	if len(rows) == 0 {
		return cardLine(summary)
	}
	return component{"tag": "collapsible_panel", "expanded": false, "header": component{"title": cardText(summary), "icon": component{"tag": "standard_icon", "token": "down-small-ccm_outlined"}, "icon_position": "right", "icon_expanded_angle": 180}, "elements": []component{cardLine(strings.Join(rows, "\n"))}}
}
func (d DigestCard) payload() component {
	items := []component{}
	color := "blue"
	if d.Highlight != "" {
		color = "orange"
		items = append(items, cardLine(d.Highlight))
	}
	items = append(items, metricPair("Claude 可用　"+d.Pool, "当前并发　"+d.Concurrency),
		metricPair("每分钟请求　"+d.RPM, "首字 P95　"+d.Latency),
		detailPanel("号池详情 · 总耗时 P95 "+d.Duration, []string{d.PoolDetail}),
		component{"tag": "hr"}, detailPanel(d.Risk, d.RiskRows))
	if len(d.Examples) > 0 {
		items = append(items, detailPanel("高频拦截 · 近 3 小时", d.Examples))
	}
	if d.SpendShare != "" {
		d.SpendRows = append([]string{"消费前三占今日实际扣费 " + d.SpendShare + "%"}, d.SpendRows...)
	}
	if d.ErrorImpact != "" {
		d.ErrorRows = append([]string{"遇到错误　" + d.ErrorImpact}, d.ErrorRows...)
	}
	items = append(items, detailPanel(d.Spend, d.SpendRows), detailPanel(d.Errors, d.ErrorRows))
	if len(d.Trend) > 0 {
		items = append(items, component{"tag": "chart", "height": "240px", "chart_spec": component{
			"type": "line", "title": component{"text": "近 24 小时用户用量（百万 Token / 小时）"},
			"data": component{"values": d.Trend}, "xField": "hour", "yField": "millions", "seriesField": "user",
			"legends": component{"visible": true, "orient": "bottom"},
		}}, detailPanel("用量前三占 "+d.TokenShare+"% · 用户明细", d.TrendRows))
	}
	if len(d.Suggestions) > 0 {
		items = append(items, cardLine("运营建议"), cardLine(strings.Join(d.Suggestions, "\n")))
	}
	return component{"schema": "2.0", "header": component{"template": color, "title": cardText(d.Title), "subtitle": cardText(d.Window)}, "body": component{"elements": items}}
}
func (c Client) SendDigestCard(ctx context.Context, hook string, d DigestCard) error {
	if d.Title == "" {
		return fmt.Errorf("missing digest title")
	}
	return c.sendPayload(ctx, hook, map[string]any{"msg_type": "interactive", "card": d.payload()})
}
