package agentcontext

import (
	"context"
	"fmt"
	mm "github.com/mattermost/mattermost/server/public/model"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/seyeon/agent-bridge/server/modelclient"
	"regexp"
	"strings"
)

type PromptBuilder interface{ BuildSystemPrompt(*agent.Agent) string }
type Builder struct {
	Prompt PromptBuilder
}

func Normalize(text, username string) string {
	if username == "" {
		return strings.TrimSpace(text)
	}
	re := regexp.MustCompile(`(^|[^a-zA-Z0-9_.@-])@` + regexp.QuoteMeta(username))
	matches := re.FindAllStringSubmatchIndex(text, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		end := m[1]
		if end < len(text) && strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-", rune(text[end])) {
			continue
		}
		// Keep the matched boundary character, remove just @username.
		start := m[3]
		text = text[:start] + text[end:]
	}
	return strings.TrimSpace(text)
}
func (b *Builder) Build(ctx context.Context, a *agent.Agent, p *mm.Post) ([]modelclient.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	system := b.Prompt.BuildSystemPrompt(a)
	if len(system) > 16*1024 {
		return nil, fmt.Errorf("system prompt too large")
	}
	username := ""
	if a.Messenger.Username != nil {
		username = *a.Messenger.Username
	}
	content := Normalize(p.Message, username)
	if content == "" {
		return nil, fmt.Errorf("empty user message")
	}
	if len(content) > 64*1024 {
		return nil, fmt.Errorf("message too large")
	}
	// Conversation history belongs to the LangGraph checkpoint. Send only this turn.
	return []modelclient.Message{{Role: "system", Content: system}, {Role: "user", Content: content}}, ctx.Err()
}
