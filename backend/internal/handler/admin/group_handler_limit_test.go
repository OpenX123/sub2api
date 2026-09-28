package admin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateGroupRequestLimitFieldsTriState(t *testing.T) {
	t.Run("omitted means unchanged", func(t *testing.T) {
		var req UpdateGroupRequest
		require.NoError(t, json.Unmarshal([]byte(`{}`), &req))
		require.Nil(t, req.DailyLimitUSD.ToServiceInput())
		require.Nil(t, req.WeeklyLimitUSD.ToServiceInput())
		require.Nil(t, req.MonthlyLimitUSD.ToServiceInput())
	})

	t.Run("null means unlimited", func(t *testing.T) {
		var req UpdateGroupRequest
		require.NoError(t, json.Unmarshal([]byte(`{"daily_limit_usd":null}`), &req))
		limit := req.DailyLimitUSD.ToServiceInput()
		require.NotNil(t, limit)
		require.Negative(t, *limit)
	})

	t.Run("number is preserved", func(t *testing.T) {
		var req UpdateGroupRequest
		require.NoError(t, json.Unmarshal([]byte(`{"weekly_limit_usd":0,"monthly_limit_usd":42.5}`), &req))
		require.Equal(t, 0.0, *req.WeeklyLimitUSD.ToServiceInput())
		require.Equal(t, 42.5, *req.MonthlyLimitUSD.ToServiceInput())
	})
}

func TestGroupRequestsAcceptFiveHourLimit(t *testing.T) {
	var create CreateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{"name":"carpool","rate_limit_5h":0}`), &create))
	require.Equal(t, 0.0, *create.RateLimit5h.ToServiceInput(), "0 表示禁止使用")

	var update UpdateGroupRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &update))
	require.Nil(t, update.RateLimit5h.ToServiceInput(), "未提交时保持原值")

	require.NoError(t, json.Unmarshal([]byte(`{"rate_limit_5h":null}`), &update))
	require.Negative(t, *update.RateLimit5h.ToServiceInput(), "null 表示不限")
}
