package feishu

import (
	"context"
	"fmt"
	"strings"
)

type DigestCard struct {
	Title, Window, Pool, Concurrency, PoolDetail string
	RPM, Latency, Duration                       string
	Highlight, Risk, Spend, Errors               string
	RiskRows, SpendRows, ErrorRows               []string
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
		component{"tag": "hr"}, detailPanel(d.Risk, d.RiskRows), detailPanel(d.Spend, d.SpendRows), detailPanel(d.Errors, d.ErrorRows))
	return component{"schema": "2.0", "header": component{"template": color, "title": cardText(d.Title), "subtitle": cardText(d.Window)}, "body": component{"elements": items}}
}
func (c Client) SendDigestCard(ctx context.Context, hook string, d DigestCard) error {
	if d.Title == "" {
		return fmt.Errorf("missing digest title")
	}
	return c.sendPayload(ctx, hook, map[string]any{"msg_type": "interactive", "card": d.payload()})
}
