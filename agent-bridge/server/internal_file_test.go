package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/seyeon/agent-bridge/server/modelclient"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestInternalFileAPIAuthenticationAndAccess(t *testing.T) {
	for _, mode := range []string{"ok", "anonymous", "browser", "wrong_token", "wrong_channel", "wrong_user", "wrong_file", "permission", "confirmation_missing", "confirmation_pending", "confirmation_wrong_file", "confirmation_expired", "confirmation_approved"} {
		t.Run(mode, func(t *testing.T) {
			uid := strings.Repeat("u", 26)
			bid := strings.Repeat("b", 26)
			cid := strings.Repeat("c", 26)
			pid := strings.Repeat("p", 26)
			fid := strings.Repeat("f", 26)
			api := &plugintest.API{}
			kv := &testKV{values: map[string][]byte{}}
			api.On("KVGet", mock.Anything).Return(kv.KVGet)
			p := &Plugin{}
			p.API = api
			p.agents = agent.NewService(agent.NewKVAgentStore(kv))
			p.setConfiguration(&configuration{LangGraphToken: "secret-runtime-token"})
			p.router = p.initRouter()
			a := agent.DefaultAgent()
			a.ID = "researcher"
			a.Lifecycle.Enabled = true
			a.Runtime.Status = "ACTIVE"
			a.Messenger.UserID = &bid
			a.Messenger.Bot = true
			a.Tools.Enabled = true
			a.Tools.Allowed = []string{"file-reader"}
			a.Permissions.Files.Read = true
			in := internalFileRequest{AgentID: a.ID, RequesterUserID: uid, ChannelID: cid, PostID: pid, FileID: fid, MaxContentBytes: 1024}
			if mode == "permission" {
				a.Permissions.Files.Read = false
			}
			if strings.HasPrefix(mode, "confirmation") {
				a.Tools.RequireConfirmation = []string{"file-reader"}
				item := pendingApproval{ID: "apr_01234567890123456789012345678901", AgentID: a.ID, SourcePostID: pid, ChannelID: cid, RequestedBy: uid, Status: "APPROVED", Decision: "approve", ExpiresAt: time.Now().Add(time.Hour), Calls: []modelclient.ApprovalCall{{ToolID: "file-reader", Name: "read_file", Args: json.RawMessage(`{"file_id":"` + fid + `"}`)}}}
				if mode == "confirmation_pending" {
					item.Status = "PENDING"
					item.Decision = ""
				}
				if mode == "confirmation_wrong_file" {
					item.Calls[0].Args = json.RawMessage(`{"file_id":"` + strings.Repeat("x", 26) + `"}`)
				}
				if mode == "confirmation_expired" {
					item.ExpiresAt = time.Now().Add(-time.Hour)
				}
				raw, _ := json.Marshal(item)
				kv.values[approvalPrefix+item.ID] = raw
				if mode != "confirmation_missing" {
					in.ApprovalID = item.ID
				}
			}
			raw, _ := json.Marshal(a)
			kv.values["agent:v1:"+a.ID] = raw
			kv.values["agent:bot:v1:"+bid] = []byte(a.ID)
			post := &model.Post{Id: pid, UserId: uid, ChannelId: cid, Message: "read this", FileIds: []string{fid}}
			api.On("HasPermissionToChannel", uid, cid, model.PermissionReadChannel).Return(true).Maybe()
			api.On("HasPermissionToChannel", bid, cid, model.PermissionReadChannel).Return(true).Maybe()
			api.On("GetPost", pid).Return(post, nil).Maybe()
			api.On("GetUser", bid).Return(&model.User{Id: bid, IsBot: true}, nil).Maybe()
			api.On("GetUser", uid).Return(&model.User{Id: uid}, nil).Maybe()
			api.On("GetChannel", cid).Return(&model.Channel{Id: cid, Type: model.ChannelTypeDirect, Name: uid + "__" + bid}, nil).Maybe()
			api.On("GetChannelMember", cid, uid).Return(&model.ChannelMember{}, nil).Maybe()
			api.On("GetChannelMember", cid, bid).Return(&model.ChannelMember{}, nil).Maybe()
			api.On("GetFileInfo", fid).Return(&model.FileInfo{Id: fid, PostId: pid, Name: "project.md", MimeType: "text/markdown", Size: 10}, nil).Maybe()
			api.On("GetFile", fid).Return([]byte("Backend Go"), nil).Maybe()
			if mode == "wrong_channel" {
				in.ChannelID = strings.Repeat("x", 26)
			}
			if mode == "wrong_user" {
				in.RequesterUserID = strings.Repeat("x", 26)
			}
			if mode == "wrong_file" {
				in.FileID = strings.Repeat("x", 26)
			}
			raw, _ = json.Marshal(in)
			req := httptest.NewRequest("POST", "/api/internal/files/read", bytes.NewReader(raw))
			if mode != "anonymous" && mode != "browser" {
				req.Header.Set("Authorization", "Bearer secret-runtime-token")
			}
			if mode == "browser" {
				req.Header.Set("Mattermost-User-ID", uid)
			}
			if mode == "wrong_token" {
				req.Header.Set("Authorization", "Bearer wrong")
			}
			w := httptest.NewRecorder()
			p.ServeHTTP(nil, w, req)
			switch mode {
			case "ok", "confirmation_approved":
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "Backend Go")
			case "anonymous", "browser", "wrong_token":
				require.Equal(t, 401, w.Code)
			default:
				require.Equal(t, 403, w.Code, w.Body.String())
			}
			if mode != "ok" && mode != "confirmation_approved" {
				api.AssertNotCalled(t, "GetFile", fid)
			}
			require.NotContains(t, w.Body.String(), "secret-runtime-token")
		})
	}
}
