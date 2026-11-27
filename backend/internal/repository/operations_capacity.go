package repository

import (
	"context"
	"time"

	"github.com/lib/pq"
)

// FiveHourWindowCosts uses the admin account usage cost basis and a separate upstream
// window start per account. The index on (account_id, created_at) serves every range.
func (r *usageLogRepository) FiveHourWindowCosts(ctx context.Context, ids []int64, starts []time.Time) (map[int64]float64, error) {
	const query = `SELECT u.account_id, SUM(COALESCE(u.account_stats_cost,u.total_cost)*COALESCE(u.account_rate_multiplier,1))
 FROM unnest($1::bigint[],$2::timestamptz[]) AS w(account_id,start_at)
 JOIN usage_logs u ON u.account_id=w.account_id AND u.created_at>=w.start_at
 GROUP BY u.account_id`
	rows, err := r.sql.QueryContext(ctx, query, pq.Array(ids), pq.Array(starts))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[int64]float64, len(ids))
	for rows.Next() {
		var id int64
		var cost float64
		if err := rows.Scan(&id, &cost); err != nil {
			return nil, err
		}
		out[id] = cost
	}
	return out, rows.Err()
}
