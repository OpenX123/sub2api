package feishu

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

func (c Client) SendIncidentCard(ctx context.Context, hook string, firing, recovered []string) error {
	color := "green"
	if len(firing) > 0 {
		color = "red"
	}
	name := strings.TrimSuffix(os.Getenv("OPERATIONS_DIGEST_TITLE"), " · 运营速报")
	if name == "" {
		name = "Sub2API"
	}
	elements := []component{}
	for _, section := range []struct {
		name string
		rows []string
	}{{"新告警", firing}, {"已恢复", recovered}} {
		if len(section.rows) == 0 {
			continue
		}
		rows := section.rows
		if len(rows) > 60 {
			rows = append(append([]string{}, rows[:60]...), fmt.Sprintf("其余 %d 项请查看后台", len(rows)-60))
		}
		elements = append(elements, cardLine(fmt.Sprintf("%s %d 项", section.name, len(section.rows))), cardLine(strings.Join(rows, "\n")))
	}
	card := component{"schema": "2.0", "header": component{"template": color, "title": cardText(name + " · 运行告警"), "subtitle": cardText(time.Now().In(time.FixedZone("CST", 8*3600)).Format("01-02 15:04 北京时间"))}, "body": component{"elements": elements}}
	return c.sendPayload(ctx, hook, map[string]any{"msg_type": "interactive", "card": card})
}
