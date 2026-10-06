package agentcontext

import (
	"context"
	mm "github.com/mattermost/mattermost/server/public/model"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/seyeon/agent-bridge/server/prompt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOnlyCurrentTurnWithoutThreadRead(t *testing.T) {
	a := agent.DefaultAgent()
	a.Messenger.UserID = mm.NewPointer("bot")
	a.Messenger.Username = mm.NewPointer("agent-researcher")
	p := &mm.Post{Id: "current", RootId: "root", ChannelId: "channel", UserId: "human", CreateAt: 30, Message: "@agent-researcher question"}
	messages, err := (&Builder{Prompt: prompt.Builder{}}).Build(context.Background(), &a, p)
	require.NoError(t, err)
	require.Len(t, messages, 2)
	require.Equal(t, "question", messages[1].Content)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (&Builder{Prompt: prompt.Builder{}}).Build(ctx, &a, p)
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
