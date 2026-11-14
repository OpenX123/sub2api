package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationObserveUsersMigration(t *testing.T) {
	raw, err := FS.ReadFile("248a_content_moderation_observe_users.sql")
	require.NoError(t, err)
	sql := string(raw)
	require.Contains(t, sql, "'content_moderation_config'")
	require.Contains(t, sql, "'cyber_policy_user_allowlist'")
	require.Contains(t, sql, "observe_user_ids")
	require.Contains(t, sql, "NOT (cfg.value::jsonb ? 'observe_user_ids')", "must be idempotent and never overwrite an explicit list")
	upper := strings.ToUpper(sql)
	for _, disallowed := range []string{"DELETE FROM", "DROP ", "TRUNCATE"} {
		require.NotContains(t, upper, disallowed)
	}
}
