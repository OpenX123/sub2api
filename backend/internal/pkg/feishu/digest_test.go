package feishu

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDigestCardKeepsDetailsCollapsed(t *testing.T) {
	d := DigestCard{Title: "运营速报", Highlight: "重复拦截", Risk: "Jev", RiskRows: []string{"audit@example.org"}, Spend: "消费", Errors: "错误"}
	card := d.payload()
	require.Equal(t, "2.0", card["schema"])
	require.Equal(t, "orange", card["header"].(component)["template"])
	items := card["body"].(component)["elements"].([]component)
	var riskPanel component
	for _, item := range items {
		if item["tag"] == "collapsible_panel" {
			require.Equal(t, false, item["expanded"])
			header := item["header"].(component)
			if header["title"].(component)["content"] == "Jev" {
				riskPanel = item
			}
		}
	}
	require.NotNil(t, riskPanel)
	require.NotContains(t, riskPanel["header"].(component)["title"].(component)["content"], "@")
	require.Contains(t, riskPanel["elements"].([]component)[0]["text"].(component)["content"], "audit@example.org")
}

func TestDigestTrendUsesNativeChartAndPlainText(t *testing.T) {
	d := DigestCard{Title: "运营速报", Trend: []TrendPoint{{Hour: "23:00", User: "#17", Millions: 1.5}},
		TokenShare: "42.0", TierRows: []string{"EDU · 额度未知 2"}, Examples: []string{"最近一例　23:10"},
		Suggestions: []string{"检查额度即将耗尽的 EDU 账号"}}
	b, err := json.Marshal(d.payload())
	require.NoError(t, err)
	for _, field := range []string{`"tag":"chart"`, `"seriesField":"user"`, `"millions":1.5`, "最近一例", "EDU · 额度未知 2", "运营建议"} {
		require.Contains(t, string(b), field)
	}
	require.NotContains(t, string(b), `"tag":"markdown"`)
}
