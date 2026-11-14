package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type PushNotificationHandler struct {
	service *service.PushNotificationService
}

func NewPushNotificationHandler(svc *service.PushNotificationService) *PushNotificationHandler {
	return &PushNotificationHandler{service: svc}
}

type pushNotificationConfigRequest struct {
	Enabled            *bool   `json:"enabled"`
	SendKey            *string `json:"send_key"`
	ClearSendKey       bool    `json:"clear_send_key"`
	HitDailyCap        *int    `json:"hit_daily_cap"`
	HitWindowMinutes   *int    `json:"hit_window_minutes"`
	DailyReportEnabled *bool   `json:"daily_report_enabled"`
	DailyReportHour    *int    `json:"daily_report_hour"`
	OpsCriticalEnabled *bool   `json:"ops_critical_enabled"`
}

func (h *PushNotificationHandler) GetConfig(c *gin.Context) {
	cfg, err := h.service.GetConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *PushNotificationHandler) UpdateConfig(c *gin.Context) {
	var req pushNotificationConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.service.UpdateConfig(c.Request.Context(), service.UpdatePushNotificationConfigInput{
		Enabled: req.Enabled, SendKey: req.SendKey, ClearSendKey: req.ClearSendKey,
		HitDailyCap: req.HitDailyCap, HitWindowMinutes: req.HitWindowMinutes,
		DailyReportEnabled: req.DailyReportEnabled, DailyReportHour: req.DailyReportHour,
		OpsCriticalEnabled: req.OpsCriticalEnabled,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *PushNotificationHandler) SendTest(c *gin.Context) {
	if err := h.service.SendTest(c.Request.Context()); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "ok"})
}
