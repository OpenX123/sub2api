package feishu

import (
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
