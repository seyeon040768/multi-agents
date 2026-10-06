package orchestrator

import (
	"errors"
	mm "github.com/mattermost/mattermost/server/public/model"
	"github.com/seyeon/agent-bridge/server/agent"
	"regexp"
	"strings"
)

type Repository interface {
	Get(string) (*agent.Agent, error)
	AgentIDForBot(string) (string, error)
}
type ResolveAPI interface {
	GetUser(string) (*mm.User, *mm.AppError)
	GetUserByUsername(string) (*mm.User, *mm.AppError)
	GetChannel(string) (*mm.Channel, *mm.AppError)
	GetChannelMember(string, string) (*mm.ChannelMember, *mm.AppError)
}
type AgentResolver interface {
	Resolve(*mm.Post) (*agent.Agent, error)
}
type Resolver struct {
	API    ResolveAPI
	Agents Repository
}

var mentions = regexp.MustCompile(`(?:^|[^a-zA-Z0-9_.@-])@([a-zA-Z0-9_.-]+)`)

func (r *Resolver) linked(userID string) (*agent.Agent, error) {
	id, err := r.Agents.AgentIDForBot(userID)
	if errors.Is(err, agent.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a, err := r.Agents.Get(id)
	if errors.Is(err, agent.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if a.Messenger.UserID == nil || *a.Messenger.UserID != userID || !a.Messenger.Bot {
		return nil, nil
	}
	return a, nil
}
func (r *Resolver) Resolve(p *mm.Post) (*agent.Agent, error) {
	if p == nil || p.IsSystemMessage() || p.DeleteAt != 0 || strings.TrimSpace(p.Message) == "" || p.GetProp(mm.PostPropsFromBot) != nil || p.GetProp("agentbridge_generated") != nil {
		return nil, nil
	}
	user, err := r.API.GetUser(p.UserId)
	if err != nil {
		return nil, err
	}
	if user.IsBot || user.DeleteAt != 0 {
		return nil, nil
	}
	channel, err := r.API.GetChannel(p.ChannelId)
	if err != nil {
		return nil, err
	}
	if channel.DeleteAt != 0 {
		return nil, nil
	}
	if _, err = r.API.GetChannelMember(p.ChannelId, p.UserId); err != nil {
		return nil, err
	}
	if channel.Type == mm.ChannelTypeDirect {
		ids := strings.Split(channel.Name, "__")
		if len(ids) != 2 {
			return nil, nil
		}
		other := ""
		if ids[0] == p.UserId {
			other = ids[1]
		} else if ids[1] == p.UserId {
			other = ids[0]
		}
		if other == "" {
			return nil, nil
		}
		return r.linked(other)
	}
	if channel.Type != mm.ChannelTypeOpen && channel.Type != mm.ChannelTypePrivate {
		return nil, nil
	}
	var selected *agent.Agent
	for _, match := range mentions.FindAllStringSubmatch(p.Message, -1) {
		if !strings.HasPrefix(match[1], "agent-") {
			continue
		}
		bot, e := r.API.GetUserByUsername(match[1])
		if e != nil {
			if e.StatusCode == 404 {
				continue
			}
			return nil, e
		}
		if !bot.IsBot || bot.DeleteAt != 0 {
			continue
		}
		a, linkErr := r.linked(bot.Id)
		if linkErr != nil {
			return nil, linkErr
		}
		if a == nil {
			continue
		}
		// A plugin must not use its unrestricted API to expose private channels to an uninvited Bot.
		if _, e = r.API.GetChannelMember(p.ChannelId, bot.Id); e != nil {
			if e.StatusCode == 404 || e.StatusCode == 403 {
				continue
			}
			return nil, e
		}
		if selected != nil && selected.ID != a.ID {
			return nil, nil
		}
		selected = a
	}
	return selected, nil
}
