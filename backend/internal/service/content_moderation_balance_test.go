package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestContentModerationFreezesKeyLongOnPaymentRequired(t *testing.T) {
	require.Equal(t, 10*time.Minute, contentModerationFreezeDurationForHTTPStatus(http.StatusPaymentRequired))
}

func TestContentModerationPaymentRequiredKeepsRealError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer server.Close()
	svc := &ContentModerationService{httpClient: server.Client(), keyHealth: map[string]*contentModerationKeyHealth{}}
	cfg := newTypeSafeTestConfig(server.URL)

	_, err := svc.callModeration(context.Background(), cfg, "需要审核的文字")
	require.EqualError(t, err, "typesafe API status 402")
	require.Equal(t, int32(1), calls.Load())

	_, err = svc.callModeration(context.Background(), cfg, "需要审核的文字")
	require.EqualError(t, err, "no moderation api key available: typesafe API status 402")
	require.Equal(t, int32(1), calls.Load(), "frozen key must not be called again")
}
