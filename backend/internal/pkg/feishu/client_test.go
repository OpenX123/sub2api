package feishu

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestBuildPost(t *testing.T) {
	msg := buildPost("日报", "### 审计概况\n| 指标 | 数值 |\n|---|---|\n| 命中 | 2 |\n[在后台查看](https://example.org/log?id=1)")
	require.Equal(t, post{Title: "日报", Content: [][]element{
		{plain("【审计概况】")}, {plain("命中："), plain("2")}, {{Tag: "a", Text: "查看审计记录", Href: "https://example.org/log?id=1"}},
	}}, msg)
}
