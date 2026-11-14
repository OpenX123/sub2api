package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func NewContentModerationReportRepository(db *sql.DB) service.ContentModerationReportRepository {
	return &contentModerationRepository{db: db}
}

// TopFlaggedUsers ranks users by flagged audits in [from, to); cyber_policy rows are excluded.
func (r *contentModerationRepository) TopFlaggedUsers(ctx context.Context, from, to time.Time, limit int) ([]service.ContentModerationUserHitCount, error) {
	rows, err := r.db.QueryContext(ctx, `
SELECT COALESCE(user_id, 0), user_email, COUNT(*) AS hits,
       COUNT(*) FILTER (WHERE action IN ('block', 'hash_block', 'keyword_block')) AS blocks
FROM content_moderation_logs
WHERE flagged AND action <> 'cyber_policy' AND created_at >= $1 AND created_at < $2
GROUP BY COALESCE(user_id, 0), user_email
ORDER BY hits DESC, 1 ASC
LIMIT $3`, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("query top flagged users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []service.ContentModerationUserHitCount
	for rows.Next() {
		var item service.ContentModerationUserHitCount
		if err := rows.Scan(&item.UserID, &item.UserEmail, &item.Hits, &item.Blocks); err != nil {
			return nil, fmt.Errorf("scan top flagged users: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
