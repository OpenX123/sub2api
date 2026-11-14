package repository

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestContentModerationTopFlaggedUsers(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	from := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)
	mock.ExpectQuery(`SELECT COALESCE\(user_id, 0\), user_email`).WithArgs(from, to, 5).
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "user_email", "hits", "blocks"}).AddRow(16, "big@example.com", 8, 0).AddRow(1, "a@example.com", 3, 3))

	got, err := NewContentModerationReportRepository(db).TopFlaggedUsers(context.Background(), from, to, 5)

	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, int64(16), got[0].UserID)
	require.Equal(t, int64(8), got[0].Hits)
	require.Equal(t, int64(3), got[1].Blocks)
	require.NoError(t, mock.ExpectationsWereMet())
}
