package agentcontext

import (
	"context"
	"fmt"
	mm "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/seyeon/agent-bridge/server/prompt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestThreadWindowAndIsolation(t *testing.T) {
	api := &plugintest.API{}
	a := agent.DefaultAgent()
	a.Messenger.UserID = mm.NewPointer("bot")
	a.Messenger.Username = mm.NewPointer("agent-researcher")
	posts := mm.NewPostList()
	for i := 0; i < 30; i++ {
		posts.AddPost(&mm.Post{Id: fmt.Sprint(i), ChannelId: "channel", UserId: "human", CreateAt: int64(i), Message: fmt.Sprint(i)})
	}
	posts.AddPost(&mm.Post{Id: "other-channel", ChannelId: "private", CreateAt: 29, Message: "secret"})
	posts.AddPost(&mm.Post{Id: "future", ChannelId: "channel", CreateAt: 99, Message: "future"})
	posts.AddPost(&mm.Post{Id: "deleted", ChannelId: "channel", CreateAt: 29, Message: "deleted", DeleteAt: 1})
	api.On("GetPostThread", "root").Return(posts, (*mm.AppError)(nil))
	p := &mm.Post{Id: "current", RootId: "root", ChannelId: "channel", UserId: "human", CreateAt: 30, Message: "@agent-researcher question"}
	messages, err := (&Builder{API: api, Prompt: prompt.Builder{}}).Build(context.Background(), &a, p)
	require.NoError(t, err)
	require.Len(t, messages, 21)
	require.Equal(t, "11", messages[1].Content)
	require.Equal(t, "question", messages[20].Content)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (&Builder{API: api, Prompt: prompt.Builder{}}).Build(ctx, &a, p)
	require.ErrorIs(t, err, context.Canceled)
}
func TestMentionNormalization(t *testing.T) {
	require.Equal(t, "question", Normalize("@agent-researcher question", "agent-researcher"))
	require.Equal(t, "x@agent-researcher email", Normalize("x@agent-researcher email", "agent-researcher"))
	require.Equal(t, "@agent-researcher2 question", Normalize("@agent-researcher2 question", "agent-researcher"))
}
func TestMentionMustMatchEntireUsername(t *testing.T) {
	for _, s := range []string{"@agent-researcher-other", "@agent-researcher.other", "@agent-researcher_extra"} {
		require.Equal(t, s, Normalize(s, "agent-researcher"))
	}
	require.Equal(t, "first  second", Normalize("@agent-researcher first @agent-researcher second", "agent-researcher"))
}
