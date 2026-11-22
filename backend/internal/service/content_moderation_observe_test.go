package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationObserveUserIsAuditedButNotBlocked(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.99, func(cfg *ContentModerationConfig) {
		cfg.ObserveUserIDs = []int64{12}
	})
	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 12, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("bad prompt")})
	require.NoError(t, err)
	require.True(t, decision.Allowed)
	require.False(t, decision.Blocked)

	task := <-svc.asyncQueue
	require.Equal(t, ContentModerationModeRiskControlLogOnly, task.log.Mode)
	require.True(t, task.log.Flagged)
}

func TestContentModerationCyberAllowlistNoLongerExemptsModeration(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.99, nil)
	settingRepo, ok := svc.settingRepo.(*contentModerationTestSettingRepo)
	require.True(t, ok)
	settingRepo.values[SettingKeyCyberPolicyUserAllowlist] = "12"

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 12, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("bad prompt")})

	require.NoError(t, err)
	require.True(t, decision.Blocked, "cyber allowlist only governs OpenAI cyber_policy now")
}

func TestContentModerationObserveUsersRoundTripThroughUpdateConfig(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.99, nil)
	ctx := context.Background()
	_, err := svc.loadRuntimeSnapshot(ctx)
	require.NoError(t, err)

	ids := []int64{22, 16, 0, 16, -3, 2}
	view, err := svc.UpdateConfig(ctx, UpdateContentModerationConfigInput{ObserveUserIDs: &ids})
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{2, 16, 22}, view.ObserveUserIDs)
	require.Contains(t, svc.runtimeSnapshot.Load().observeUsers, int64(16))

	empty := []int64{}
	view, err = svc.UpdateConfig(ctx, UpdateContentModerationConfigInput{ObserveUserIDs: &empty})
	require.NoError(t, err)
	require.Empty(t, view.ObserveUserIDs)
	require.Empty(t, svc.runtimeSnapshot.Load().observeUsers)
}
