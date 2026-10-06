package main

import (
	"context"
	"encoding/json"
	mm "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestHookEnqueuesOnlyHumanAgentRequests(t *testing.T) {
	api := &plugintest.API{}
	a := agent.DefaultAgent()
	a.ID = "researcher"
	a.Runtime.Status = "ACTIVE"
	a.Messenger.UserID = mm.NewPointer("bot")
	a.Messenger.Username = mm.NewPointer("agent-researcher")
	raw, err := json.Marshal(a)
	require.NoError(t, err)
	api.On("GetUser", "human").Return(&mm.User{Id: "human"}, (*mm.AppError)(nil))
	api.On("GetChannel", "dm").Return(&mm.Channel{Type: mm.ChannelTypeDirect, Name: "human__bot"}, (*mm.AppError)(nil))
	api.On("GetChannelMember", "dm", "human").Return(&mm.ChannelMember{}, (*mm.AppError)(nil))
	api.On("KVGet", "agent:bot:v1:bot").Return([]byte("researcher"), (*mm.AppError)(nil))
	api.On("KVGet", "agent:v1:researcher").Return(raw, (*mm.AppError)(nil))
	p := &Plugin{}
	p.API = api
	p.chatCtx = context.Background()
	p.chatQueue = make(chan *mm.Post, 1)
	post := &mm.Post{Id: "question", ChannelId: "dm", UserId: "human", Message: "hello"}
	p.MessageHasBeenPosted(nil, post)
	require.Len(t, p.chatQueue, 1)
	require.NotSame(t, post, <-p.chatQueue)
	p.MessageHasBeenPosted(nil, &mm.Post{Message: "reply", Props: mm.StringInterface{"agentbridge_generated": true}})
	require.Empty(t, p.chatQueue)
	p.MessageHasBeenPosted(nil, post)
	api.On("LogError", "Agent message queue full", "post_id", "question").Return().Once()
	api.On("CreatePost", mock.MatchedBy(func(reply *mm.Post) bool {
		return reply.RootId == "question" && reply.GetProp("agentbridge_error") == true
	})).Return(&mm.Post{}, (*mm.AppError)(nil)).Once()
	p.MessageHasBeenPosted(nil, post)
	require.Len(t, p.chatQueue, 1)
	api.AssertExpectations(t)
}
func TestWorkerShutdown(t *testing.T) {
	p := &Plugin{}
	p.startChat()
	require.NoError(t, p.OnDeactivate())
	require.ErrorIs(t, p.chatCtx.Err(), context.Canceled)
}
