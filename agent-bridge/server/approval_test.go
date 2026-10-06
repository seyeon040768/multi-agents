package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/seyeon/agent-bridge/server/modelclient"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func approvalFixture() *pendingApproval {
	return &pendingApproval{ID: "apr_01234567890123456789012345678901", AgentID: "researcher", RootPostID: "root", SourcePostID: "source", ChannelID: "channel", PostID: "approval-post", RequestedBy: "requester", Status: "PENDING", CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(30 * time.Minute), Calls: []modelclient.ApprovalCall{{ToolID: "debug-echo", Name: "debug_echo", ToolCallID: "call-1", Args: json.RawMessage(`{"text":"hello"}`)}}}
}
func approvalPlugin(item *pendingApproval) (*Plugin, *testKV, *plugintest.API) {
	api := &plugintest.API{}
	kv := &testKV{values: map[string][]byte{}}
	raw, _ := json.Marshal(item)
	kv.values[approvalPrefix+item.ID] = raw
	api.On("KVGet", mock.Anything).Return(kv.KVGet)
	api.On("KVSetWithOptions", mock.Anything, mock.Anything, mock.Anything).Return(true, (*model.AppError)(nil))
	api.On("KVCompareAndSet", mock.Anything, mock.Anything, mock.Anything).Return(kv.KVCompareAndSet)
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	p := &Plugin{chatCtx: context.Background(), approvalQueue: make(chan *pendingApproval, 64)}
	p.API = api
	p.router = p.initRouter()
	return p, kv, api
}
func approvalClick(p *Plugin, item *pendingApproval, actor, claimed, post, decision string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(model.PostActionIntegrationRequest{UserId: claimed, PostId: post, ChannelId: item.ChannelID, Context: map[string]any{"approval_id": item.ID}})
	r := httptest.NewRequest("POST", "/api/v1/approvals/"+item.ID+"/"+decision, bytes.NewReader(body))
	if actor != "" {
		r.Header.Set("Mattermost-User-ID", actor)
	}
	w := httptest.NewRecorder()
	p.ServeHTTP(nil, w, r)
	return w
}
func TestApprovalActorAuthenticationAndMembership(t *testing.T) {
	for _, tc := range []struct {
		name, actor, claimed, roles, post string
		member                            bool
		status                            int
	}{
		{"anonymous", "", "requester", "system_user", "approval-post", true, 401},
		{"spoofed actor", "other", "requester", "system_user", "approval-post", true, 403},
		{"other user", "other", "other", "system_user", "approval-post", true, 403},
		{"wrong post", "requester", "requester", "system_user", "other-post", true, 403},
		{"not channel member", "requester", "requester", "system_user", "approval-post", false, 403},
		{"requester", "requester", "requester", "system_user", "approval-post", true, 200},
		{"admin", "admin", "admin", "system_user system_admin", "approval-post", true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := approvalFixture()
			p, _, api := approvalPlugin(item)
			api.On("GetUser", tc.actor).Return(&model.User{Id: tc.actor, Roles: tc.roles}, (*model.AppError)(nil))
			var err *model.AppError
			if !tc.member {
				err = model.NewAppError("test", "missing", nil, "", 404)
			}
			api.On("GetChannelMember", item.ChannelID, tc.actor).Return(&model.ChannelMember{}, err)
			w := approvalClick(p, item, tc.actor, tc.claimed, tc.post, "approve")
			require.Equal(t, tc.status, w.Code, w.Body.String())
			if tc.status == 200 {
				require.Len(t, p.approvalQueue, 1)
			} else {
				require.Empty(t, p.approvalQueue)
			}
		})
	}
}
func TestConcurrentApprovalCASAllowsExactlyOneDecision(t *testing.T) {
	item := approvalFixture()
	p, kv, api := approvalPlugin(item)
	api.On("GetUser", "requester").Return(&model.User{Id: "requester", Roles: "system_user"}, (*model.AppError)(nil))
	api.On("GetChannelMember", item.ChannelID, "requester").Return(&model.ChannelMember{}, (*model.AppError)(nil))
	var wg sync.WaitGroup
	results := make(chan int, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- approvalClick(p, item, "requester", "requester", item.PostID, "approve").Code
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for code := range results {
		if code == 200 {
			successes++
		} else {
			require.Equal(t, 409, code)
		}
	}
	require.Equal(t, 1, successes)
	require.Len(t, p.approvalQueue, 1)
	raw, _ := kv.KVGet(approvalPrefix + item.ID)
	var stored pendingApproval
	require.NoError(t, json.Unmarshal(raw, &stored))
	require.Equal(t, "APPROVED", stored.Status)
	require.Equal(t, "requester", stored.DecidedBy)
	require.Equal(t, item.Calls, stored.Calls)
}
func TestExpiredApprovalQueuesExpiryInsteadOfExecution(t *testing.T) {
	item := approvalFixture()
	item.ExpiresAt = time.Now().Add(-time.Minute)
	p, _, api := approvalPlugin(item)
	api.On("GetUser", "requester").Return(&model.User{Id: "requester"}, (*model.AppError)(nil))
	api.On("GetChannelMember", item.ChannelID, "requester").Return(&model.ChannelMember{}, (*model.AppError)(nil))
	require.Equal(t, 200, approvalClick(p, item, "requester", "requester", item.PostID, "approve").Code)
	queued := <-p.approvalQueue
	require.Equal(t, "expire", queued.Decision)
	require.Equal(t, "EXPIRED", queued.Status)
}
func TestApprovalPublicationContainsOnlyOpaqueIDInActions(t *testing.T) {
	item := approvalFixture()
	p, kv, api := approvalPlugin(item)
	delete(kv.values, approvalPrefix+item.ID)
	api.On("CreatePost", mock.MatchedBy(func(post *model.Post) bool {
		require.Contains(t, post.Message, `{"text":"hello"}`)
		require.Equal(t, "root", post.RootId)
		attachments := post.Props["attachments"].([]*model.SlackAttachment)
		for _, action := range attachments[0].Actions {
			require.Equal(t, map[string]any{"approval_id": item.ID}, action.Integration.Context)
			require.Contains(t, action.Integration.URL, item.ID)
			require.NotContains(t, action.Integration.URL, "root")
		}
		return true
	})).Return(&model.Post{Id: item.PostID}, (*model.AppError)(nil)).Once()
	a := agent.DefaultAgent()
	a.ID = item.AgentID
	a.Messenger.UserID = model.NewPointer("bot")
	in := &modelclient.ApprovalInterrupt{Type: "tool_approval", ApprovalID: item.ID, Calls: item.Calls, CreatedAt: item.CreatedAt, ExpiresAt: item.ExpiresAt}
	source := &model.Post{Id: item.SourcePostID, RootId: item.RootPostID, ChannelId: item.ChannelID, UserId: item.RequestedBy}
	require.NoError(t, p.Request(context.Background(), &a, source, in))
	require.NoError(t, p.Request(context.Background(), &a, source, in))
	api.AssertNumberOfCalls(t, "CreatePost", 1)
}

func TestApprovalQueueFullRollsBackUnconsumedDecision(t *testing.T) {
	item := approvalFixture()
	p, kv, _ := approvalPlugin(item)
	p.approvalQueue = make(chan *pendingApproval)
	old, _ := kv.KVGet(approvalPrefix + item.ID)
	item.Status = "APPROVED"
	item.Decision = "approve"
	require.False(t, p.queueApproval(old, item))
	after, _ := kv.KVGet(approvalPrefix + item.ID)
	require.Equal(t, old, after)
}
func TestPendingDecisionRecoveryAfterPluginRestart(t *testing.T) {
	item := approvalFixture()
	item.Status = "APPROVED"
	item.Decision = "approve"
	p, _, api := approvalPlugin(item)
	api.On("KVList", 0, 100).Return([]string{approvalPrefix + item.ID}, (*model.AppError)(nil))
	p.recoverApprovalDecisions()
	require.Len(t, p.approvalQueue, 1)
	recovered := <-p.approvalQueue
	require.Equal(t, item.ID, recovered.ID)
	require.Equal(t, item.Calls, recovered.Calls)
	require.Equal(t, "approve", recovered.Decision)
}
func TestExpirySweep(t *testing.T) {
	item := approvalFixture()
	item.ExpiresAt = time.Now().Add(-time.Minute)
	p, _, api := approvalPlugin(item)
	api.On("KVList", 0, 100).Return([]string{approvalPrefix + item.ID}, (*model.AppError)(nil))
	p.expireApprovals()
	require.Len(t, p.approvalQueue, 1)
	require.Equal(t, "expire", (<-p.approvalQueue).Decision)
}
func TestApprovalWorkerUpdatesPostAndRepliesInOriginalThread(t *testing.T) {
	for _, status := range []string{"EXECUTED", "REJECTED", "FAILED"} {
		t.Run(status, func(t *testing.T) {
			item := approvalFixture()
			item.Decision = "approve"
			item.Status = "APPROVED"
			if status == "REJECTED" {
				item.Decision = "reject"
				item.Status = "REJECTED"
			}
			p, kv, api := approvalPlugin(item)
			a := agent.DefaultAgent()
			a.ID = item.AgentID
			a.Runtime.Status = "ACTIVE"
			a.Messenger.UserID = model.NewPointer("bot")
			raw, _ := json.Marshal(a)
			kv.values["agent:v1:"+a.ID] = raw
			p.agents = agent.NewService(agent.NewKVAgentStore(kv))
			post := &model.Post{Id: item.PostID, RootId: item.RootPostID, ChannelId: item.ChannelID, Message: "pending", Props: model.StringInterface{"attachments": []any{1}, "agentbridge_generated": true}}
			api.On("GetPost", item.PostID).Return(func(string) (*model.Post, *model.AppError) { return post.Clone(), nil })
			api.On("UpdatePost", mock.Anything).Return(func(updated *model.Post) (*model.Post, *model.AppError) { post = updated.Clone(); return updated, nil })
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/resume", r.URL.Path)
				require.Equal(t, "Bearer token", r.Header.Get("Authorization"))
				var body modelclient.ResumeRequest
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, item.ID, body.ApprovalID)
				require.Equal(t, item.Decision, body.Decision)
				require.Equal(t, item.RootPostID, body.RootPostID)
				require.Equal(t, a.Tools, body.Tools)
				_ = json.NewEncoder(w).Encode(&modelclient.GenerateResponse{Text: "result", ApprovalStatus: status})
			}))
			defer runtime.Close()
			p.setConfiguration(&configuration{LangGraphURL: runtime.URL, LangGraphToken: "token"})
			api.On("CreatePost", mock.MatchedBy(func(reply *model.Post) bool {
				return reply.RootId == item.RootPostID && reply.ChannelId == item.ChannelID && reply.UserId == "bot" && reply.Message == "result"
			})).Return(&model.Post{}, (*model.AppError)(nil))
			p.processApproval(item)
			p.processApproval(item) // Duplicate recovery work must not call runtime or post twice.
			after, _ := kv.KVGet(approvalPrefix + item.ID)
			var saved pendingApproval
			require.NoError(t, json.Unmarshal(after, &saved))
			require.Equal(t, status, saved.Status)
			require.NotNil(t, saved.FinishedAt)
			require.Nil(t, post.GetProp("attachments"))
			require.NotEqual(t, "pending", post.Message)
			api.AssertNumberOfCalls(t, "CreatePost", 1)
		})
	}
}
