package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/feishu"
)

const (
	defaultPushHitDailyCap      = 300
	maxPushHitDailyCap          = 1000
	defaultPushHitWindowMinutes = 30
	minPushHitWindowMinutes     = 5
	maxPushHitWindowMinutes     = 240
	defaultPushDailyReportHour  = 9
)

type PushNotificationConfig struct {
	Enabled             bool   `json:"enabled"`
	SendKey             string `json:"send_key,omitempty"`
	HitDailyCap         int    `json:"hit_daily_cap"`
	HitWindowMinutes    int    `json:"hit_window_minutes"`
	DailyReportEnabled  bool   `json:"daily_report_enabled"`
	DailyReportHour     int    `json:"daily_report_hour"`
	OpsCriticalEnabled  bool   `json:"ops_critical_enabled"`
	AccountAlertEnabled bool   `json:"account_alert_enabled"`
}

type PushNotificationConfigView struct {
	Enabled             bool   `json:"enabled"`
	SendKeyConfigured   bool   `json:"send_key_configured"`
	SendKeyMasked       string `json:"send_key_masked"`
	HitDailyCap         int    `json:"hit_daily_cap"`
	HitWindowMinutes    int    `json:"hit_window_minutes"`
	DailyReportEnabled  bool   `json:"daily_report_enabled"`
	DailyReportHour     int    `json:"daily_report_hour"`
	OpsCriticalEnabled  bool   `json:"ops_critical_enabled"`
	AccountAlertEnabled bool   `json:"account_alert_enabled"`
}

type UpdatePushNotificationConfigInput struct {
	Enabled             *bool   `json:"enabled"`
	SendKey             *string `json:"send_key"`
	ClearSendKey        bool    `json:"clear_send_key"`
	HitDailyCap         *int    `json:"hit_daily_cap"`
	HitWindowMinutes    *int    `json:"hit_window_minutes"`
	DailyReportEnabled  *bool   `json:"daily_report_enabled"`
	DailyReportHour     *int    `json:"daily_report_hour"`
	OpsCriticalEnabled  *bool   `json:"ops_critical_enabled"`
	AccountAlertEnabled *bool   `json:"account_alert_enabled"`
}

func defaultPushNotificationConfig() *PushNotificationConfig {
	return &PushNotificationConfig{
		HitDailyCap: defaultPushHitDailyCap, HitWindowMinutes: defaultPushHitWindowMinutes,
		DailyReportEnabled: true, DailyReportHour: defaultPushDailyReportHour, OpsCriticalEnabled: true,
		AccountAlertEnabled: true,
	}
}

func clampPushInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (c *PushNotificationConfig) normalize() {
	c.SendKey = strings.TrimSpace(c.SendKey)
	if c.HitDailyCap <= 0 {
		c.HitDailyCap = defaultPushHitDailyCap
	}
	c.HitDailyCap = clampPushInt(c.HitDailyCap, 1, maxPushHitDailyCap)
	if c.HitWindowMinutes <= 0 {
		c.HitWindowMinutes = defaultPushHitWindowMinutes
	}
	c.HitWindowMinutes = clampPushInt(c.HitWindowMinutes, minPushHitWindowMinutes, maxPushHitWindowMinutes)
	c.DailyReportHour = clampPushInt(c.DailyReportHour, 0, 23)
}

func parsePushNotificationConfig(raw string) (*PushNotificationConfig, error) {
	cfg := defaultPushNotificationConfig()
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), cfg); err != nil {
			return nil, fmt.Errorf("parse push notification config: %w", err)
		}
	}
	if cfg.SendKey != "" && !feishu.ValidWebhook(cfg.SendKey) {
		cfg.SendKey = ""
		cfg.Enabled = false
	}
	cfg.normalize()
	return cfg, nil
}

func (c *PushNotificationConfig) view() *PushNotificationConfigView {
	v := &PushNotificationConfigView{
		Enabled: c.Enabled, SendKeyConfigured: c.SendKey != "",
		HitDailyCap: c.HitDailyCap, HitWindowMinutes: c.HitWindowMinutes,
		DailyReportEnabled: c.DailyReportEnabled, DailyReportHour: c.DailyReportHour, OpsCriticalEnabled: c.OpsCriticalEnabled,
		AccountAlertEnabled: c.AccountAlertEnabled,
	}
	if c.SendKey != "" {
		v.SendKeyMasked = maskSecretTail(c.SendKey)
	}
	return v
}

func (s *PushNotificationService) readConfig(ctx context.Context) (*PushNotificationConfig, error) {
	if s == nil || s.settingRepo == nil {
		return defaultPushNotificationConfig(), nil
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyPushNotificationConfig)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return nil, fmt.Errorf("get push notification config: %w", err)
	}
	return parsePushNotificationConfig(raw)
}

func (s *PushNotificationService) GetConfig(ctx context.Context) (*PushNotificationConfigView, error) {
	cfg, err := s.readConfig(ctx)
	if err != nil {
		return nil, err
	}
	return cfg.view(), nil
}

func (s *PushNotificationService) UpdateConfig(ctx context.Context, in UpdatePushNotificationConfigInput) (*PushNotificationConfigView, error) {
	cfg, err := s.readConfig(ctx)
	if err != nil {
		return nil, err
	}
	if in.ClearSendKey {
		cfg.SendKey = ""
	} else if in.SendKey != nil && strings.TrimSpace(*in.SendKey) != "" {
		key := strings.TrimSpace(*in.SendKey)
		if !feishu.ValidWebhook(key) {
			return nil, infraerrors.BadRequest("INVALID_PUSH_SEND_KEY", "飞书 Webhook 地址格式不正确")
		}
		cfg.SendKey = key
	}
	if in.Enabled != nil {
		cfg.Enabled = *in.Enabled
	}
	if in.HitDailyCap != nil {
		cfg.HitDailyCap = *in.HitDailyCap
	}
	if in.HitWindowMinutes != nil {
		cfg.HitWindowMinutes = *in.HitWindowMinutes
	}
	if in.DailyReportEnabled != nil {
		cfg.DailyReportEnabled = *in.DailyReportEnabled
	}
	if in.DailyReportHour != nil {
		cfg.DailyReportHour = *in.DailyReportHour
	}
	if in.OpsCriticalEnabled != nil {
		cfg.OpsCriticalEnabled = *in.OpsCriticalEnabled
	}
	if in.AccountAlertEnabled != nil {
		cfg.AccountAlertEnabled = *in.AccountAlertEnabled
	}
	cfg.normalize()
	if cfg.Enabled && cfg.SendKey == "" {
		return nil, infraerrors.BadRequest("PUSH_SEND_KEY_REQUIRED", "启用推送前需要填写 飞书 Webhook")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal push notification config: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyPushNotificationConfig, string(raw)); err != nil {
		return nil, fmt.Errorf("save push notification config: %w", err)
	}
	return cfg.view(), nil
}

// SendTest sends synchronously, bypassing the queue, daily caps and the enabled switch.
func (s *PushNotificationService) SendTest(ctx context.Context) error {
	cfg, err := s.readConfig(ctx)
	if err != nil {
		return err
	}
	if cfg.SendKey == "" {
		return infraerrors.BadRequest("PUSH_SEND_KEY_REQUIRED", "请先填写并保存 飞书 Webhook")
	}
	if s.sender == nil {
		return errors.New("push sender unavailable")
	}
	if err := s.sender.Send(ctx, cfg.SendKey, "【测试】sub2api 推送测试", "- 通知渠道：飞书机器人\n- 消息格式：原生富文本"); err != nil {
		return infraerrors.BadRequest("PUSH_TEST_FAILED", "推送失败："+err.Error())
	}
	return nil
}
