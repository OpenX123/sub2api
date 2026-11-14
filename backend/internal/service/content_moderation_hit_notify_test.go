package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestContentModerationPersistNotifiesFlaggedHit(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.99, nil)
	notifier := &fakeModerationNotifier{}
	svc.SetNotifier(notifier)

	decision, err := svc.Check(context.Background(), ContentModerationCheckInput{
		RequestID: "req-hit", UserID: 7, UserEmail: "seven@example.com", GroupName: "x1", Model: "claude-opus-5-5",
		Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("bad prompt"),
	})
	require.NoError(t, err)
	require.True(t, decision.Blocked)

	// pre_block persists flagged logs through the async queue
	task := <-svc.asyncQueue
	svc.persistContentModerationLog(context.Background(), task.config, task.log, task.inputHash, task.recordHash, task.applySideEffects)

	require.Eventually(t, func() bool { return len(notifier.snapshotHits()) == 1 }, time.Second, 10*time.Millisecond)
	hit := notifier.snapshotHits()[0]
	require.Equal(t, int64(7), hit.UserID)
	require.Equal(t, "seven@example.com", hit.UserEmail)
	require.Equal(t, "sexual", hit.Category)
	require.Equal(t, ContentModerationActionBlock, hit.Action)
	require.Equal(t, "req-hit", hit.RequestID)
	require.Equal(t, task.inputHash, hit.InputHash)
	require.False(t, hit.Observe)
}

func TestContentModerationPersistSkipsNonFlagged(t *testing.T) {
	svc, _, _ := newOpenAIModerationTestService(t, 0.0, func(cfg *ContentModerationConfig) { cfg.RecordNonHits = true })
	notifier := &fakeModerationNotifier{}
	svc.SetNotifier(notifier)
	_, err := svc.Check(context.Background(), ContentModerationCheckInput{UserID: 7, Protocol: ContentModerationProtocolOpenAIChat, Body: moderationChatBody("hello")})
	require.NoError(t, err)
	task := <-svc.asyncQueue
	require.False(t, task.log.Flagged)
	svc.persistContentModerationLog(context.Background(), task.config, task.log, task.inputHash, task.recordHash, task.applySideEffects)
	require.Empty(t, notifier.snapshotHits())
}
