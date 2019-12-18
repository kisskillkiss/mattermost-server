// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package app

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/mattermost/mattermost-server/v5/mlog"
	"github.com/mattermost/mattermost-server/v5/model"
	"github.com/mattermost/mattermost-server/v5/store"
)

// CreateDefaultMemberships adds users to teams and channels based on their group memberships and how those groups are
// configured to sync with teams and channels for group members on or after the given timestamp.
func (a *App) CreateDefaultMemberships(since int64) error {
	teamMembers, appErr := a.TeamMembersToAdd(since)
	if appErr != nil {
		return appErr
	}

	for _, userTeam := range teamMembers {
		_, err := a.AddTeamMember(userTeam.TeamID, userTeam.UserID)
		if err != nil {
			return err
		}

		a.Log.Info("added teammember",
			mlog.String("user_id", userTeam.UserID),
			mlog.String("team_id", userTeam.TeamID),
		)
	}

	channelMembers, appErr := a.ChannelMembersToAdd(since)
	if appErr != nil {
		return appErr
	}

	for _, userChannel := range channelMembers {
		channel, err := a.GetChannel(userChannel.ChannelID)
		if err != nil {
			return err
		}

		tmem, err := a.GetTeamMember(channel.TeamId, userChannel.UserID)
		if err != nil && err.Id != "store.sql_team.get_member.missing.app_error" {
			return err
		}

		// First add user to team
		if tmem == nil {
			_, err = a.AddTeamMember(channel.TeamId, userChannel.UserID)
			if err != nil {
				return err
			}
			a.Log.Info("added teammember",
				mlog.String("user_id", userChannel.UserID),
				mlog.String("team_id", channel.TeamId),
			)
		}

		_, err = a.AddChannelMember(userChannel.UserID, channel, "", "")
		if err != nil {
			if err.Id == "api.channel.add_user.to.channel.failed.deleted.app_error" {
				a.Log.Info("Not adding user to channel because they have already left the team",
					mlog.String("user_id", userChannel.UserID),
					mlog.String("channel_id", userChannel.ChannelID),
				)
			} else {
				return err
			}
		}

		a.Log.Info("added channelmember",
			mlog.String("user_id", userChannel.UserID),
			mlog.String("channel_id", userChannel.ChannelID),
		)
	}

	return nil
}

// DeleteGroupConstrainedMemberships deletes team and channel memberships of users who aren't members of the allowed
// groups of all group-constrained teams and channels.
func (a *App) DeleteGroupConstrainedMemberships() error {
	channelMembers, appErr := a.ChannelMembersToRemove()
	if appErr != nil {
		return appErr
	}

	for _, userChannel := range channelMembers {
		channel, err := a.GetChannel(userChannel.ChannelId)
		if err != nil {
			return err
		}

		err = a.RemoveUserFromChannel(userChannel.UserId, "", channel)
		if err != nil {
			return err
		}

		a.Log.Info("removed channelmember",
			mlog.String("user_id", userChannel.UserId),
			mlog.String("channel_id", channel.Id),
		)
	}

	teamMembers, appErr := a.TeamMembersToRemove()
	if appErr != nil {
		return appErr
	}

	for _, userTeam := range teamMembers {
		err := a.RemoveUserFromTeam(userTeam.TeamId, userTeam.UserId, "")
		if err != nil {
			return err
		}

		a.Log.Info("removed teammember",
			mlog.String("user_id", userTeam.UserId),
			mlog.String("team_id", userTeam.TeamId),
		)
	}

	return nil
}

// SyncSyncableRoles updates the SchemeAdmin field value of the given syncable's members based on the configuration of
// the member's group memberships and the configuration of those groups to the syncable.
func (a *App) SyncSyncableRoles(syncableID string, syncableType model.GroupSyncableType) *model.AppError {
	permittedAdmins, err := a.Srv.Store.Group().PermittedSyncableAdmins(syncableID, syncableType)
	if err != nil {
		return err
	}

	a.Log.Info(
		fmt.Sprintf("Permitted admins for %s", syncableType),
		mlog.String(strings.ToLower(fmt.Sprintf("%s_id", syncableType)), syncableID),
		mlog.Any("permitted_admins", permittedAdmins),
	)

	var updateFunc func(string, []string, store.Equality, bool) *model.AppError

	switch syncableType {
	case model.GroupSyncableTypeTeam:
		updateFunc = a.Srv.Store.Team().UpdateMembersRole
	case model.GroupSyncableTypeChannel:
		updateFunc = a.Srv.Store.Channel().UpdateMembersRole
	default:
		return model.NewAppError("App.SyncSyncableRoles", "groups.unsupported_syncable_type", map[string]interface{}{"Value": syncableType}, "", http.StatusInternalServerError)
	}

	err = updateFunc(syncableID, permittedAdmins, store.Equals, true)
	if err != nil {
		return err
	}

	err = updateFunc(syncableID, permittedAdmins, store.NotEquals, false)
	if err != nil {
		return err
	}

	return nil
}
