package feishu

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDigestCardKeepsDetailsCollapsed(t *testing.T) {
	d := DigestCard{Title: "运营速报", Highlight: "重复拦截", Risk: "Jev", RiskRows: []string{"audit@example.org"}, Spend: "消费", Errors: "错误"}
	card := d.payload()
	require.Equal(t, "2.0", card["schema"])
	header, ok := card["header"].(component)
	require.True(t, ok)
	require.Equal(t, "orange", header["template"])
	body, ok := card["body"].(component)
	require.True(t, ok)
	items, ok := body["elements"].([]component)
	require.True(t, ok)
	var riskPanel component
	for _, item := range items {
		if item["tag"] == "collapsible_panel" {
			require.Equal(t, false, item["expanded"])
			header, ok := item["header"].(component)
			require.True(t, ok)
			if reflect.DeepEqual(header["title"], cardText("Jev")) {
				riskPanel = item
			}
		}
	}
	require.NotNil(t, riskPanel)
	require.Equal(t, []component{cardLine("audit@example.org")}, riskPanel["elements"])
}

func TestDigestTrendUsesNativeChartAndPlainText(t *testing.T) {
	d := DigestCard{Title: "运营速报", Trend: []TrendPoint{{Hour: "23:00", User: "星河", Millions: 1.5}},
		TokenShare: "42.0", Examples: []string{"最近一例　23:10"},
		Suggestions: []string{"检查额度即将耗尽的 EDU 账号"}}
	b, err := json.Marshal(d.payload())
	require.NoError(t, err)
	for _, field := range []string{`"tag":"chart"`, `"seriesField":"user"`, `"millions":1.5`, "最近一例", "星河", "运营建议"} {
		require.Contains(t, string(b), field)
	}
	require.NotContains(t, string(b), `"tag":"markdown"`)
}
