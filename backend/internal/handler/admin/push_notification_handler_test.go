package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type pushMemorySettingRepo struct{ values map[string]string }

func (r *pushMemorySettingRepo) Get(_ context.Context, key string) (*service.Setting, error) {
	if v, ok := r.values[key]; ok {
		return &service.Setting{Key: key, Value: v}, nil
	}
	return nil, service.ErrSettingNotFound
}

func (r *pushMemorySettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if v, ok := r.values[key]; ok {
		return v, nil
	}
	return "", service.ErrSettingNotFound
}

func (r *pushMemorySettingRepo) Set(_ context.Context, key, value string) error {
	r.values[key] = value
	return nil
}

func (r *pushMemorySettingRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := r.values[k]; ok {
			out[k] = v
		}
	}
	return out, nil
}

func (r *pushMemorySettingRepo) SetMultiple(_ context.Context, settings map[string]string) error {
	for k, v := range settings {
		r.values[k] = v
	}
	return nil
}

func (r *pushMemorySettingRepo) GetAll(_ context.Context) (map[string]string, error) {
	return r.values, nil
}

func (r *pushMemorySettingRepo) Delete(_ context.Context, key string) error {
	delete(r.values, key)
	return nil
}

func TestPushNotificationHandlerUpdateHidesKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := service.NewPushNotificationService(&pushMemorySettingRepo{values: map[string]string{}}, nil, nil)
	h := NewPushNotificationHandler(svc)
	router := gin.New()
	router.PUT("/push/config", h.UpdateConfig)
	router.GET("/push/config", h.GetConfig)

	body := `{"enabled":true,"send_key":"https://open.feishu.cn/open-apis/bot/v2/hook/00000000-0000-0000-0000-00000000f123","hit_daily_cap":200}`
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/push/config", strings.NewReader(body)))

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"send_key_configured":true`)
	require.Contains(t, w.Body.String(), `"hit_daily_cap":200`)
	require.NotContains(t, w.Body.String(), "https://open.feishu.cn/open-apis/bot/v2/hook/00000000-0000-0000-0000-00000000f123")

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/push/config", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "https://open.feishu.cn/open-apis/bot/v2/hook/00000000-0000-0000-0000-00000000f123")
}
