package fileaccess

import (
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestReadAccessAndValidation(t *testing.T) {
	for _, mode := range []string{"ok", "disabled", "not_allowed", "denied", "permission", "other_channel", "no_bot_member", "read_permission", "deleted_bot", "not_attached", "wrong_post", "missing", "large", "image", "binary", "bad_utf8", "large_download", "truncate"} {
		t.Run(mode, func(t *testing.T) {
			api := &plugintest.API{}
			a := agent.DefaultAgent()
			a.Tools.Enabled = true
			a.Tools.Allowed = []string{"file-reader"}
			a.Permissions.Files.Read = true
			a.Runtime.Status = "ACTIVE"
			a.Lifecycle.Enabled = true
			bot := "bot"
			a.Messenger.UserID = &bot
			a.Messenger.Bot = true
			post := &model.Post{Id: "post", UserId: "user", ChannelId: "channel", FileIds: []string{"file"}}
			info := &model.FileInfo{Id: "file", PostId: "post", Name: "project.md", MimeType: "text/markdown", Size: 10}
			content := []byte("Backend Go")
			switch mode {
			case "disabled":
				a.Tools.Enabled = false
			case "not_allowed":
				a.Tools.Allowed = nil
			case "denied":
				a.Tools.Denied = []string{"file-reader"}
			case "permission":
				a.Permissions.Files.Read = false
			case "other_channel":
				post.ChannelId = "private"
			case "not_attached":
				post.FileIds = nil
			case "wrong_post":
				info.PostId = "other"
			case "large":
				info.Size = MaxBytes + 1
			case "image":
				info.Name = "image.png"
				info.MimeType = "image/png"
			case "binary":
				content = []byte{0, 1, 2}
			case "bad_utf8":
				content = []byte{0xff}
			case "large_download":
				content = []byte(strings.Repeat("x", MaxBytes+1))
			case "truncate":
				content = []byte("한국어 텍스트")
			}
			api.On("HasPermissionToChannel", "user", "channel", model.PermissionReadChannel).Return(mode != "read_permission").Maybe()
			api.On("HasPermissionToChannel", "bot", "channel", model.PermissionReadChannel).Return(true).Maybe()
			api.On("GetUser", "user").Return(&model.User{Id: "user"}, nil).Maybe()
			botUser := &model.User{Id: "bot", IsBot: true}
			if mode == "deleted_bot" {
				botUser.DeleteAt = 1
			}
			api.On("GetUser", "bot").Return(botUser, nil).Maybe()
			api.On("GetChannel", "channel").Return(&model.Channel{Id: "channel"}, nil).Maybe()
			api.On("GetChannelMember", "channel", "user").Return(&model.ChannelMember{}, nil).Maybe()
			if mode == "no_bot_member" {
				api.On("GetChannelMember", "channel", "bot").Return((*model.ChannelMember)(nil), model.NewAppError("test", "test", nil, "denied", 403))
			} else {
				api.On("GetChannelMember", "channel", "bot").Return(&model.ChannelMember{}, nil).Maybe()
			}
			if mode == "missing" {
				api.On("GetFileInfo", "file").Return((*model.FileInfo)(nil), model.NewAppError("test", "test", nil, "missing", 404))
			} else {
				api.On("GetFileInfo", "file").Return(info, nil).Maybe()
			}
			api.On("GetFile", "file").Return(content, nil).Maybe()
			limit := 100
			if mode == "truncate" {
				limit = 7
			}
			result, err := (Service{API: api}).Read(&a, post, "user", "channel", "file", limit)
			if mode == "ok" || mode == "truncate" {
				require.NoError(t, err)
				require.Equal(t, mode == "truncate", result.Truncated)
				if mode == "truncate" {
					require.Equal(t, "한국", result.Content)
				}
			} else {
				require.Error(t, err)
				var safe *Error
				require.ErrorAs(t, err, &safe)
			}
			if mode != "ok" && mode != "truncate" && mode != "binary" && mode != "bad_utf8" && mode != "large_download" {
				api.AssertNotCalled(t, "GetFile", "file")
			}
		})
	}
}
func TestAttachmentMetadataDoesNotReadContent(t *testing.T) {
	api := &plugintest.API{}
	api.On("GetFileInfo", "file").Return(&model.FileInfo{Id: "file", PostId: "post", Name: "project.md", MimeType: "text/markdown", Size: 10}, nil)
	api.On("GetFileInfo", "other").Return(&model.FileInfo{Id: "other", PostId: "different"}, nil)
	result := (Service{API: api}).Attachments(&model.Post{Id: "post", FileIds: []string{"file", "other"}})
	require.Len(t, result, 1)
	require.Equal(t, "file", result[0].FileID)
	api.AssertNotCalled(t, "GetFile", "file")
}
