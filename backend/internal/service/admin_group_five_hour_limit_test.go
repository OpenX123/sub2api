//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminService_CreateGroup_PersistsFiveHourLimit(t *testing.T) {
	repo := &groupRepoStubForAdmin{}
	svc := &adminServiceImpl{groupRepo: repo}
	limit := 8.5

	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{
		Name:             "carpool",
		Platform:         PlatformAnthropic,
		RateMultiplier:   1,
		SubscriptionType: SubscriptionTypeSubscription,
		RateLimit5h:      &limit,
	})

	require.NoError(t, err)
	require.NotNil(t, repo.created.RateLimit5h)
	require.Equal(t, 8.5, *repo.created.RateLimit5h)
}

func TestAdminService_UpdateGroup_FiveHourLimitTriState(t *testing.T) {
	limit := 10.0
	existing := &Group{ID: 1, Name: "carpool", Platform: PlatformAnthropic, Status: StatusActive, RateLimit5h: &limit}
	repo := &groupRepoStubForAdmin{getByID: existing}
	svc := &adminServiceImpl{groupRepo: repo}

	description := "untouched"
	group, err := svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{Description: &description})
	require.NoError(t, err)
	require.Equal(t, 10.0, *group.RateLimit5h, "未提交 5h 限额时保持原值")

	zero := 0.0
	group, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{RateLimit5h: &zero})
	require.NoError(t, err)
	require.NotNil(t, group.RateLimit5h, "0 表示禁止使用，不能被当成不限")
	require.Equal(t, 0.0, *group.RateLimit5h)

	unlimited := -1.0
	group, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{RateLimit5h: &unlimited})
	require.NoError(t, err)
	require.Nil(t, group.RateLimit5h, "null（handler 转成负数）表示不限")
}

func TestAdminService_DuplicateGroupCopiesFiveHourLimit(t *testing.T) {
	limit := 6.0
	cloned := cloneGroupForDuplicate(&Group{ID: 1, Name: "carpool", Platform: PlatformAnthropic, RateLimit5h: &limit}, "op")
	require.NotNil(t, cloned.RateLimit5h)
	require.Equal(t, 6.0, *cloned.RateLimit5h)
	*cloned.RateLimit5h = 1
	require.Equal(t, 6.0, limit, "复制出的分组不能与源分组共享指针")
}
