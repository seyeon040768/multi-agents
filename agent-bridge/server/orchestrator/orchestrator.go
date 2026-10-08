package orchestrator

import (
	"context"
	"errors"
	"fmt"
	mm "github.com/mattermost/mattermost/server/public/model"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/seyeon/agent-bridge/server/modelclient"
)

type ContextBuilder interface {
	Build(context.Context, *agent.Agent, *mm.Post) ([]modelclient.Message, error)
}
type Messenger interface {
	Reply(context.Context, *agent.Agent, *mm.Post, string, bool) error
}
type ApprovalHandler interface {
	Request(context.Context, *agent.Agent, *mm.Post, *modelclient.ApprovalInterrupt) error
}
type Logger interface{ LogError(string, ...interface{}) }
type Orchestrator struct {
	Resolver  AgentResolver
	Context   ContextBuilder
	Models    modelclient.Client
	Messenger Messenger
	Logger    Logger
	Approvals ApprovalHandler
}

func (o *Orchestrator) HandleMessage(ctx context.Context, p *mm.Post) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a, err := o.Resolver.Resolve(p)
	if err != nil {
		return err
	}
	if a == nil || !a.Lifecycle.Enabled || a.Runtime.Status != "ACTIVE" {
		return nil
	}
	messages, err := o.Context.Build(ctx, a, p)
	var response *modelclient.GenerateResponse
	if err == nil {
		rootID := p.RootId
		if rootID == "" {
			rootID = p.Id
		}
		response, err = o.Models.Generate(ctx, modelclient.GenerateRequest{Tools: a.Tools, AgentID: a.ID, RootPostID: rootID, PostID: p.Id, Provider: a.Model.Provider, Model: a.Model.Name, Messages: messages, Temperature: a.Model.Parameters.Temperature, MaxTokens: a.Model.Parameters.MaxTokens, MaxContextTokens: a.Context.MaxContextTokens, TopP: a.Model.Parameters.TopP})
	}
	if err == nil && (response == nil || (response.Text == "" && response.Status != "interrupted")) {
		err = fmt.Errorf("empty model response")
	}
	if err != nil {
		// Log only controlled errors, never provider response bodies, keys or prompts.
		o.Logger.LogError("Agent request failed", "agent_id", a.ID, "provider", a.Model.Provider, "error", err.Error())
		if !errors.Is(ctx.Err(), context.Canceled) {
			if e := o.Messenger.Reply(context.WithoutCancel(ctx), a, p, "요청을 처리하는 중 오류가 발생했습니다. 잠시 후 다시 시도해 주세요.", true); e != nil {
				return e
			}
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Refresh the authoritative state after a long generation (disable/delete may have occurred).
	current, err := o.Resolver.Resolve(p)
	if err != nil {
		return err
	}
	if current == nil || current.ID != a.ID || !current.Lifecycle.Enabled || current.Runtime.Status != "ACTIVE" {
		return nil
	}
	if response.Status == "interrupted" {
		if o.Approvals == nil {
			return fmt.Errorf("approval handler unavailable")
		}
		return o.Approvals.Request(ctx, current, p, response.Interrupt)
	}
	return o.Messenger.Reply(ctx, current, p, response.Text, false)
}
