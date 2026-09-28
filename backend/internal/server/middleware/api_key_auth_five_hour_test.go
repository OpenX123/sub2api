//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthFiveHourLimitReturns429WithRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	limit := 10.0
	group := &service.Group{
		ID:               43,
		Name:             "carpool",
		Status:           service.StatusActive,
		Hydrated:         true,
		SubscriptionType: service.SubscriptionTypeSubscription,
		RateLimit5h:      &limit,
	}
	user := &service.User{ID: 8, Role: service.RoleUser, Status: service.StatusActive, Concurrency: 3}
	apiKey := &service.APIKey{ID: 101, UserID: user.ID, Key: "carpool-key", Status: service.StatusActive, User: user, Group: group}
	apiKey.GroupID = &group.ID

	apiKeyRepo := &stubApiKeyRepo{
		getByKey: func(ctx context.Context, key string) (*service.APIKey, error) {
			if key != apiKey.Key {
				return nil, service.ErrAPIKeyNotFound
			}
			clone := *apiKey
			return &clone, nil
		},
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	apiKeyService := service.NewAPIKeyService(apiKeyRepo, nil, nil, nil, nil, nil, cfg)

	now := time.Now()
	windowStart := now.Add(-time.Hour)
	sub := &service.UserSubscription{
		ID:                 56,
		UserID:             user.ID,
		GroupID:            group.ID,
		Status:             service.SubscriptionStatusActive,
		StartsAt:           now.Add(-24 * time.Hour),
		ExpiresAt:          now.Add(24 * time.Hour),
		DailyWindowStart:   &now,
		WeeklyWindowStart:  &now,
		MonthlyWindowStart: &now,
		Window5hStart:      &windowStart,
		Usage5hUSD:         10,
	}
	subscriptionRepo := &stubUserSubscriptionRepo{
		getActive: func(ctx context.Context, userID, groupID int64) (*service.UserSubscription, error) {
			clone := *sub
			return &clone, nil
		},
	}
	subscriptionService := service.NewSubscriptionService(nil, subscriptionRepo, nil, nil, cfg)
	router := newAuthTestRouter(apiKeyService, subscriptionService, cfg)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.Header.Set("x-api-key", apiKey.Key)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), "USAGE_LIMIT_EXCEEDED")
	retryAfter, err := strconv.Atoi(w.Header().Get("Retry-After"))
	require.NoError(t, err, "5h 超限应返回 Retry-After")
	require.InDelta(t, 4*60*60, retryAfter, 5, "窗口起点 1h 前，剩余约 4h 重置")
}
