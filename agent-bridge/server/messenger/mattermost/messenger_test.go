package mattermost

import (
	"context"
	mm "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNewQuestionStartsReplyThread(t *testing.T) {
	api := &plugintest.API{}
	a := agent.DefaultAgent()
	a.Messenger.UserID = mm.NewPointer("bot")
	api.On("CreatePost", mock.MatchedBy(func(p *mm.Post) bool {
		return p.RootId == "question" && p.UserId == "bot" && p.GetProp("agentbridge_generated") == true
	})).Return(&mm.Post{}, (*mm.AppError)(nil))
	require.NoError(t, (&Messenger{API: api}).Reply(context.Background(), &a, &mm.Post{Id: "question", ChannelId: "channel"}, "answer", false))
	api.AssertExpectations(t)
}
