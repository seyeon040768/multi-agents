package orchestrator

import (
	"context"
	"errors"
	mm "github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/seyeon/agent-bridge/server/agent"
	agentcontext "github.com/seyeon/agent-bridge/server/context"
	"github.com/seyeon/agent-bridge/server/messenger/mattermost"
	"github.com/seyeon/agent-bridge/server/modelclient"
	"github.com/seyeon/agent-bridge/server/prompt"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"testing"
)

type repo struct {
	a   *agent.Agent
	err error
}

func (r repo) Get(id string) (*agent.Agent, error) { return r.a, r.err }
func (r repo) AgentIDForBot(id string) (string, error) {
	if id == "bot" {
		return "researcher", r.err
	}
	return "", agent.ErrNotFound
}
func fixture() *agent.Agent {
	a := agent.DefaultAgent()
	a.ID = "researcher"
	a.Lifecycle.Enabled = true
	a.Runtime.Status = "ACTIVE"
	a.Model.Provider = "google"
	a.Model.Name = "gemini-3-flash-preview"
	a.Prompts.Identity = "Research carefully"
	a.Messenger.UserID = mm.NewPointer("bot")
	a.Messenger.Username = mm.NewPointer("agent-researcher")
	return &a
}
func nf() *mm.AppError { return mm.NewAppError("test", "missing", nil, "missing", 404) }
func TestResolverRoutesAndFilters(t *testing.T) {
	for _, tc := range []struct {
		name              string
		kind              mm.ChannelType
		msg               string
		bot, member, want bool
	}{
		{"DM", mm.ChannelTypeDirect, "question", false, true, true},
		{"mention", mm.ChannelTypeOpen, "@agent-researcher question", false, true, true},
		{"uninvited private", mm.ChannelTypePrivate, "@agent-researcher question", false, false, false},
		{"ordinary", mm.ChannelTypeOpen, "question", false, true, false},
		{"email", mm.ChannelTypeOpen, "x@agent-researcher question", false, true, false},
		{"prefix", mm.ChannelTypeOpen, "@agent-researcher-other question", false, true, false},
		{"bot", mm.ChannelTypeDirect, "question", true, true, false},
		{"group", mm.ChannelTypeGroup, "@agent-researcher question", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &plugintest.API{}
			api.On("GetUser", "human").Return(&mm.User{Id: "human", IsBot: tc.bot}, (*mm.AppError)(nil))
			api.On("GetChannel", "channel").Return(&mm.Channel{Id: "channel", Type: tc.kind, Name: "human__bot"}, (*mm.AppError)(nil))
			api.On("GetChannelMember", "channel", "human").Return(&mm.ChannelMember{}, (*mm.AppError)(nil))
			api.On("GetUserByUsername", "agent-researcher").Return(&mm.User{Id: "bot", IsBot: true}, (*mm.AppError)(nil))
			api.On("GetUserByUsername", "agent-researcher-other").Return((*mm.User)(nil), nf())
			var e *mm.AppError
			if !tc.member {
				e = nf()
			}
			api.On("GetChannelMember", "channel", "bot").Return(&mm.ChannelMember{}, e)
			a, err := (&Resolver{API: api, Agents: repo{a: fixture()}}).Resolve(&mm.Post{UserId: "human", ChannelId: "channel", Message: tc.msg})
			require.NoError(t, err)
			require.Equal(t, tc.want, a != nil)
		})
	}
	for _, p := range []*mm.Post{nil, {Type: "system_join_channel"}, {Message: "hello", Props: mm.StringInterface{"from_bot": "true"}}, {Message: "hello", Props: mm.StringInterface{"agentbridge_generated": true}}} {
		a, err := (&Resolver{API: &plugintest.API{}, Agents: repo{a: fixture()}}).Resolve(p)
		require.NoError(t, err)
		require.Nil(t, a)
	}
}

type fakeClient struct {
	t     *testing.T
	err   error
	calls int
}

func (f *fakeClient) Generate(ctx context.Context, r modelclient.GenerateRequest) (*modelclient.GenerateResponse, error) {
	f.calls++
	require.Equal(f.t, "google", r.Provider)
	require.Equal(f.t, "gemini-3-flash-preview", r.Model)
	require.Equal(f.t, "researcher", r.AgentID)
	require.Equal(f.t, "root", r.RootPostID)
	require.Equal(f.t, "current", r.PostID)
	require.Len(f.t, r.Messages, 2)
	require.Equal(f.t, "system", r.Messages[0].Role)
	require.Equal(f.t, "user", r.Messages[1].Role)
	require.Equal(f.t, "follow up", r.Messages[1].Content)
	if f.err != nil {
		return nil, f.err
	}
	return &modelclient.GenerateResponse{Text: "answer"}, nil
}
func TestMessagePipelineAndLoopPrevention(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			api := &plugintest.API{}
			a := fixture()
			r := &Resolver{API: api, Agents: repo{a: a}}
			api.On("GetUser", "human").Return(&mm.User{Id: "human"}, (*mm.AppError)(nil))
			api.On("GetChannel", "channel").Return(&mm.Channel{Type: mm.ChannelTypeDirect, Name: "human__bot"}, (*mm.AppError)(nil))
			api.On("GetChannelMember", "channel", "human").Return(&mm.ChannelMember{}, (*mm.AppError)(nil))
			p := &mm.Post{Id: "current", RootId: "root", UserId: "human", ChannelId: "channel", Message: "@agent-researcher follow up", CreateAt: 3}
			f := &fakeClient{t: t}
			if fail {
				f.err = errors.New("controlled error")
				api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
			}
			var reply *mm.Post
			api.On("CreatePost", mock.MatchedBy(func(p *mm.Post) bool {
				reply = p
				return p.UserId == "bot" && p.RootId == "root" && p.ChannelId == "channel"
			})).Return(&mm.Post{}, (*mm.AppError)(nil)).Once()
			o := &Orchestrator{Resolver: r, Context: &agentcontext.Builder{Prompt: prompt.Builder{}}, Models: f, Messenger: &mattermost.Messenger{API: api}, Logger: api}
			err := o.HandleMessage(context.Background(), p)
			require.Equal(t, fail, err != nil)
			require.Equal(t, 1, f.calls)
			if fail {
				require.NotContains(t, reply.Message, "controlled error")
				require.Equal(t, true, reply.GetProp("agentbridge_error"))
			} else {
				require.Equal(t, "answer", reply.Message)
			}
			require.NoError(t, o.HandleMessage(context.Background(), reply))
			require.Equal(t, 1, f.calls)
			a.Lifecycle.Enabled = false
			require.NoError(t, o.HandleMessage(context.Background(), p))
			require.Equal(t, 1, f.calls)
			api.AssertExpectations(t)
		})
	}
}

type resolverFunc func(*mm.Post) (*agent.Agent, error)

func (f resolverFunc) Resolve(p *mm.Post) (*agent.Agent, error) { return f(p) }
func TestDisabledAndProvisioningIgnored(t *testing.T) {
	for _, status := range []string{"DISABLED", "ERROR", "PROVISIONING", "DELETING"} {
		a := fixture()
		a.Runtime.Status = status
		o := Orchestrator{Resolver: resolverFunc(func(*mm.Post) (*agent.Agent, error) { return a, nil })}
		require.NoError(t, o.HandleMessage(context.Background(), &mm.Post{}))
	}
}

type multiRepo map[string]*agent.Agent

func (r multiRepo) Get(id string) (*agent.Agent, error) {
	for _, a := range r {
		if a.ID == id {
			return a, nil
		}
	}
	return nil, agent.ErrNotFound
}
func (r multiRepo) AgentIDForBot(id string) (string, error) {
	if a := r[id]; a != nil {
		return a.ID, nil
	}
	return "", agent.ErrNotFound
}
func TestMultipleAgentMentionsIgnored(t *testing.T) {
	api := &plugintest.API{}
	a := fixture()
	b := fixture()
	b.ID = "reviewer"
	b.Messenger.UserID = mm.NewPointer("bot-reviewer")
	api.On("GetUser", "human").Return(&mm.User{Id: "human"}, (*mm.AppError)(nil))
	api.On("GetChannel", "channel").Return(&mm.Channel{Type: mm.ChannelTypeOpen}, (*mm.AppError)(nil))
	api.On("GetChannelMember", "channel", mock.Anything).Return(&mm.ChannelMember{}, (*mm.AppError)(nil))
	api.On("GetUserByUsername", "agent-researcher").Return(&mm.User{Id: "bot", IsBot: true}, (*mm.AppError)(nil))
	api.On("GetUserByUsername", "agent-reviewer").Return(&mm.User{Id: "bot-reviewer", IsBot: true}, (*mm.AppError)(nil))
	result, err := (&Resolver{API: api, Agents: multiRepo{"bot": a, "bot-reviewer": b}}).Resolve(&mm.Post{UserId: "human", ChannelId: "channel", Message: "@agent-researcher @agent-reviewer question"})
	require.NoError(t, err)
	require.Nil(t, result)
}
func TestCancelledRequestDoesNotCallResolver(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o := Orchestrator{}
	require.ErrorIs(t, o.HandleMessage(ctx, &mm.Post{}), context.Canceled)
}

type clientFunc func(context.Context, modelclient.GenerateRequest) (*modelclient.GenerateResponse, error)

func (f clientFunc) Generate(ctx context.Context, req modelclient.GenerateRequest) (*modelclient.GenerateResponse, error) {
	return f(ctx, req)
}

type messengerFunc func(context.Context, *agent.Agent, *mm.Post, string, bool) error

func (f messengerFunc) Reply(ctx context.Context, a *agent.Agent, p *mm.Post, text string, failed bool) error {
	return f(ctx, a, p, text, failed)
}
func TestRootAndReplyThreadIdentifiers(t *testing.T) {
	for _, root := range []string{"", "root"} {
		t.Run(root, func(t *testing.T) {
			a := fixture()
			o := Orchestrator{
				Resolver: resolverFunc(func(*mm.Post) (*agent.Agent, error) { return a, nil }),
				Context:  &agentcontext.Builder{Prompt: prompt.Builder{}},
				Models: clientFunc(func(_ context.Context, req modelclient.GenerateRequest) (*modelclient.GenerateResponse, error) {
					want := root
					if want == "" {
						want = "post"
					}
					require.Equal(t, want, req.RootPostID)
					require.Equal(t, "post", req.PostID)
					require.Equal(t, a.ID, req.AgentID)
					return &modelclient.GenerateResponse{Text: "answer"}, nil
				}),
				Messenger: messengerFunc(func(context.Context, *agent.Agent, *mm.Post, string, bool) error { return nil }),
			}
			require.NoError(t, o.HandleMessage(context.Background(), &mm.Post{Id: "post", RootId: root, Message: "question"}))
		})
	}
}
