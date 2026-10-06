package agentcontext

import (
	"context"
	"fmt"
	mm "github.com/mattermost/mattermost/server/public/model"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/seyeon/agent-bridge/server/modelclient"
	"regexp"
	"sort"
	"strings"
)

type API interface {
	GetPostThread(string) (*mm.PostList, *mm.AppError)
}
type PromptBuilder interface{ BuildSystemPrompt(*agent.Agent) string }
type Builder struct {
	API    API
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
	posts := []*mm.Post{}
	if p.RootId != "" {
		thread, err := b.API.GetPostThread(p.RootId)
		if err != nil {
			return nil, err
		}
		for _, v := range thread.Posts {
			if v != nil && v.Id != p.Id && v.ChannelId == p.ChannelId && v.DeleteAt == 0 && !v.IsSystemMessage() && (v.CreateAt < p.CreateAt || (v.CreateAt == p.CreateAt && v.Id < p.Id)) && v.GetProp("agentbridge_error") == nil {
				posts = append(posts, v)
			}
		}
	}
	sort.Slice(posts, func(i, j int) bool {
		if posts[i].CreateAt == posts[j].CreateAt {
			return posts[i].Id < posts[j].Id
		}
		return posts[i].CreateAt < posts[j].CreateAt
	})
	if len(posts) > 19 {
		posts = posts[len(posts)-19:]
	}
	posts = append(posts, p)
	messages := []modelclient.Message{}
	remaining := 64 * 1024
	for i := len(posts) - 1; i >= 0; i-- {
		v := posts[i]
		content := Normalize(v.Message, username)
		if content == "" {
			continue
		}
		if len(content) > remaining {
			if v.Id == p.Id {
				return nil, fmt.Errorf("message too large")
			}
			break
		}
		role := "user"
		if a.Messenger.UserID != nil && v.UserId == *a.Messenger.UserID {
			role = "assistant"
		}
		messages = append(messages, modelclient.Message{Role: role, Content: content})
		remaining -= len(content)
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	if len(messages) == 0 || messages[len(messages)-1].Role != "user" {
		return nil, fmt.Errorf("empty user message")
	}
	return append([]modelclient.Message{{Role: "system", Content: system}}, messages...), ctx.Err()
}
