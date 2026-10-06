package mattermost

import (
	"bytes"
	"image"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func notFound() *model.AppError { return model.NewAppError("test", "not_found", nil, "not found", 404) }
func TestIndependentBots(t *testing.T) {
	api := &plugintest.API{}
	p := NewBotProvisioner(api)
	for _, id := range []string{"researcher", "reviewer"} {
		a := agent.DefaultAgent()
		a.ID = id
		a.Name = id
		a.DisplayName = "Display " + id
		a.Description = "Description " + id
		api.On("GetUserByUsername", "agent-"+id).Return((*model.User)(nil), notFound()).Once()
		api.On("CreateBot", mock.MatchedBy(func(bot *model.Bot) bool {
			return bot.Username == "agent-"+id && bot.DisplayName == a.DisplayName && bot.Description == a.Description
		})).Return(&model.Bot{UserId: "user-" + id, Username: "agent-" + id}, (*model.AppError)(nil)).Once()
		info, err := p.Create(&a)
		require.NoError(t, err)
		require.Equal(t, "user-"+id, info.UserID)
	}
	api.AssertNotCalled(t, "EnsureBotUser", mock.Anything)
	api.AssertExpectations(t)
}
func TestExistingOwnedBotAndProfile(t *testing.T) {
	api := &plugintest.API{}
	p := NewBotProvisioner(api)
	a := agent.DefaultAgent()
	a.ID = "researcher"
	a.Name = "Name"
	a.DisplayName = "Display"
	a.Description = "New description"
	userID := "bot-id"
	a.Messenger.UserID = &userID
	bot := &model.Bot{UserId: userID, Username: "agent-researcher", OwnerId: pluginID}
	api.On("GetBot", userID, true).Return(bot, (*model.AppError)(nil))
	api.On("GetUserByUsername", bot.Username).Return(&model.User{Id: userID}, (*model.AppError)(nil))
	api.On("PatchBot", userID, mock.MatchedBy(func(patch *model.BotPatch) bool {
		return *patch.DisplayName == a.DisplayName && *patch.Description == a.Description
	})).Return(bot, (*model.AppError)(nil)).Once()
	api.On("SetProfileImage", userID, mock.MatchedBy(func(data []byte) bool { _, _, err := image.DecodeConfig(bytes.NewReader(data)); return err == nil })).Return((*model.AppError)(nil)).Once()
	_, err := p.Update(&a)
	require.NoError(t, err)
	api.On("UpdateBotActive", userID, false).Return(bot, (*model.AppError)(nil)).Once()
	require.NoError(t, p.SetActive(userID, false))
	api.On("PermanentDeleteBot", userID).Return((*model.AppError)(nil)).Once()
	require.NoError(t, p.Delete(userID))
	api.AssertExpectations(t)
}
func TestForeignUsernameCannotBeAdopted(t *testing.T) {
	for _, owner := range []string{"", "other-plugin"} {
		t.Run(owner, func(t *testing.T) {
			api := &plugintest.API{}
			p := NewBotProvisioner(api)
			a := agent.DefaultAgent()
			a.ID = "researcher"
			api.On("GetUserByUsername", "agent-researcher").Return(&model.User{Id: "foreign"}, (*model.AppError)(nil))
			if owner == "" {
				api.On("GetBot", "foreign", true).Return((*model.Bot)(nil), notFound())
			} else {
				api.On("GetBot", "foreign", true).Return(&model.Bot{OwnerId: owner}, (*model.AppError)(nil))
			}
			_, err := p.Create(&a)
			require.Error(t, err)
			api.AssertNotCalled(t, "CreateBot", mock.Anything)
			api.AssertNotCalled(t, "PatchBot", mock.Anything, mock.Anything)
			if owner == "" {
				require.NoError(t, p.Delete("foreign"))
			} else {
				require.Error(t, p.Delete("foreign"))
			}
			api.AssertNotCalled(t, "PermanentDeleteBot", mock.Anything)
		})
	}
}
func TestDeleteAlreadyMissingBot(t *testing.T) {
	api := &plugintest.API{}
	api.On("GetBot", "missing", true).Return((*model.Bot)(nil), notFound())
	require.NoError(t, NewBotProvisioner(api).Delete("missing"))
}
func TestPrivateAvatarRejected(t *testing.T) {
	client := publicImageClient()
	defer client.CloseIdleConnections()
	_, err := client.Get("https://127.0.0.1/avatar.png")
	require.Error(t, err)
	_, err = client.Get("https://[::1]/avatar.png")
	require.Error(t, err)
}
