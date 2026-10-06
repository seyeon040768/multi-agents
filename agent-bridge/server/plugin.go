package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"
	"github.com/pkg/errors"

	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/seyeon/agent-bridge/server/command"
	agentcontext "github.com/seyeon/agent-bridge/server/context"
	"github.com/seyeon/agent-bridge/server/messenger/mattermost"
	"github.com/seyeon/agent-bridge/server/modelclient"
	"github.com/seyeon/agent-bridge/server/orchestrator"
	"github.com/seyeon/agent-bridge/server/prompt"
	"github.com/seyeon/agent-bridge/server/store/kvstore"
)

// Plugin implements the interface expected by the Mattermost server to communicate between the server and plugin processes.
type Plugin struct {
	plugin.MattermostPlugin
	agents      *agent.Service
	chatCtx     context.Context
	chatCancel  context.CancelFunc
	chatQueue   chan *model.Post
	chatWorkers sync.WaitGroup

	// kvstore is the client used to read/write KV records for this plugin.
	kvstore kvstore.KVStore

	// client is the Mattermost server API client.
	client *pluginapi.Client

	// commandClient is the client used to register and execute slash commands.
	commandClient command.Command

	// router is the HTTP router for handling API requests.
	router *mux.Router

	backgroundJob *cluster.Job

	// configurationLock synchronizes access to the configuration.
	configurationLock sync.RWMutex

	// configuration is the active plugin configuration. Consult getConfiguration and
	// setConfiguration for usage.
	configuration *configuration
}

// OnActivate is invoked when the plugin is activated. If an error is returned, the plugin will be deactivated.
func (p *Plugin) OnActivate() error {
	p.client = pluginapi.NewClient(p.API, p.Driver)

	p.kvstore = kvstore.NewKVStore(p.client)

	p.commandClient = command.NewCommandHandler(p.client)

	bots := mattermost.NewBotProvisioner(p.API)
	p.agents = agent.NewServiceWithBots(agent.NewKVAgentStore(p.API), bots, bots.Lock)
	p.router = p.initRouter()

	job, err := cluster.Schedule(
		p.API,
		"BackgroundJob",
		cluster.MakeWaitForRoundedInterval(1*time.Hour),
		p.runJob,
	)
	if err != nil {
		return errors.Wrap(err, "failed to schedule background job")
	}

	p.backgroundJob = job
	p.startChat()

	return nil
}

// OnDeactivate is invoked when the plugin is deactivated.
func (p *Plugin) OnDeactivate() error {
	if p.chatCancel != nil {
		p.chatCancel()
		p.chatWorkers.Wait()
	}
	if p.backgroundJob != nil {
		if err := p.backgroundJob.Close(); err != nil {
			p.API.LogError("Failed to close background job", "err", err)
		}
	}
	return nil
}

// This will execute the commands that were registered in the NewCommandHandler function.
func (p *Plugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	response, err := p.commandClient.Handle(args)
	if err != nil {
		return nil, model.NewAppError("ExecuteCommand", "plugin.command.execute_command.app_error", nil, err.Error(), http.StatusInternalServerError)
	}
	return response, nil
}

// See https://developers.mattermost.com/extend/plugins/server/reference/

// A bounded queue keeps network generation out of the message hook.
func (p *Plugin) startChat() {
	p.chatCtx, p.chatCancel = context.WithCancel(context.Background())
	p.chatQueue = make(chan *model.Post, 64)
	for i := 0; i < 4; i++ {
		p.chatWorkers.Add(1)
		go func() {
			defer p.chatWorkers.Done()
			for {
				select {
				case <-p.chatCtx.Done():
					return
				case post := <-p.chatQueue:
					cfg := p.getConfiguration()
					store := agent.NewKVAgentStore(p.API)
					o := &orchestrator.Orchestrator{Resolver: &orchestrator.Resolver{API: p.API, Agents: store}, Context: &agentcontext.Builder{Prompt: prompt.Builder{}}, Models: &modelclient.LangGraph{URL: cfg.LangGraphURL, Token: cfg.LangGraphToken}, Messenger: &mattermost.Messenger{API: p.API}, Logger: p.API}
					ctx, cancel := context.WithTimeout(p.chatCtx, 90*time.Second)
					if err := o.HandleMessage(ctx, post); err != nil {
						p.API.LogError("Agent message processing failed", "post_id", post.Id)
					}
					cancel()
				}
			}
		}()
	}
}
func (p *Plugin) MessageHasBeenPosted(_ *plugin.Context, post *model.Post) {
	if post == nil || post.IsSystemMessage() || post.GetProp(model.PostPropsFromBot) != nil || post.GetProp("agentbridge_generated") != nil || p.chatQueue == nil {
		return
	}
	// Resolve before enqueueing so unrelated channel messages cannot crowd out Agent requests.
	resolver := &orchestrator.Resolver{API: p.API, Agents: agent.NewKVAgentStore(p.API)}
	a, err := resolver.Resolve(post)
	if err != nil {
		p.API.LogError("Agent target resolution failed", "post_id", post.Id)
		return
	}
	if a == nil || !a.Lifecycle.Enabled || a.Runtime.Status != "ACTIVE" {
		return
	}
	copyPost := post.Clone()
	select {
	case <-p.chatCtx.Done():
		return
	case p.chatQueue <- copyPost:
	default:
		p.API.LogError("Agent message queue full", "post_id", post.Id)
		if err := (&mattermost.Messenger{API: p.API}).Reply(p.chatCtx, a, post, "요청이 많아 처리하지 못했습니다. 잠시 후 다시 시도해 주세요.", true); err != nil {
			p.API.LogError("Agent busy reply failed", "post_id", post.Id)
		}
	}
}
