package mattermost

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"
	"github.com/seyeon/agent-bridge/server/agent"
)

const pluginID = "com.seyeon.agentbridge"

type BotProvisioner struct{ api plugin.API }

func NewBotProvisioner(api plugin.API) *BotProvisioner { return &BotProvisioner{api: api} }
func (p *BotProvisioner) Lock(id string) (func(), error) {
	mutex, err := cluster.NewMutex(p.api, "agent-bot:v1:"+id)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := mutex.LockWithContext(ctx); err != nil {
		return nil, err
	}
	return mutex.Unlock, nil
}
func (p *BotProvisioner) owned(userID string) (*model.Bot, error) {
	bot, err := p.api.GetBot(userID, true)
	if err != nil {
		return nil, err
	}
	if bot.OwnerId != pluginID {
		return nil, fmt.Errorf("bot is not owned by Agent Bridge")
	}
	return bot, nil
}
func (p *BotProvisioner) Create(a *agent.Agent) (*agent.BotInfo, error) {
	username := "agent-" + a.ID
	existing, appErr := p.api.GetUserByUsername(username)
	if appErr != nil && appErr.StatusCode != http.StatusNotFound {
		return nil, appErr
	}
	if existing != nil {
		if _, err := p.owned(existing.Id); err != nil {
			return nil, fmt.Errorf("bot username is already in use")
		}
	}
	displayName := strings.TrimSpace(a.DisplayName)
	if a.Messenger.Profile.DisplayName != nil && strings.TrimSpace(*a.Messenger.Profile.DisplayName) != "" {
		displayName = strings.TrimSpace(*a.Messenger.Profile.DisplayName)
	}
	if displayName == "" {
		displayName = a.Name
	}
	// EnsureBotUser tracks one bot per plugin, so use CreateBot/PatchBot for per-Agent bots.
	var bot *model.Bot
	if existing != nil {
		bot, appErr = p.api.PatchBot(existing.Id, &model.BotPatch{DisplayName: &displayName, Description: &a.Description})
	} else {
		bot, appErr = p.api.CreateBot(&model.Bot{Username: username, DisplayName: displayName, Description: a.Description})
	}
	if appErr != nil {
		return nil, fmt.Errorf("Mattermost bot provisioning failed: %w", appErr)
	}
	return &agent.BotInfo{UserID: bot.UserId, Username: bot.Username}, nil
}
func (p *BotProvisioner) Update(a *agent.Agent) (*agent.BotInfo, error) {
	if a.Messenger.UserID == nil {
		return p.Create(a)
	}
	bot, err := p.owned(*a.Messenger.UserID)
	if err != nil {
		return nil, err
	}
	if bot.Username != "agent-"+a.ID {
		return nil, fmt.Errorf("bot username does not match agent")
	}
	info, err := p.Create(a)
	if err != nil {
		return nil, err
	}
	if info.UserID != *a.Messenger.UserID {
		return nil, fmt.Errorf("bot identity changed")
	}
	if err := p.syncAvatar(a, info.UserID); err != nil {
		return nil, err
	}
	return info, nil
}
func (p *BotProvisioner) SetActive(userID string, active bool) error {
	if _, err := p.owned(userID); err != nil {
		return err
	}
	_, err := p.api.UpdateBotActive(userID, active)
	if err != nil {
		return err
	}
	return nil
}
func (p *BotProvisioner) Delete(userID string) error {
	_, err := p.owned(userID)
	if appErr, ok := err.(*model.AppError); ok && appErr.StatusCode == http.StatusNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	if err := p.api.PermanentDeleteBot(userID); err != nil {
		return err
	}
	return nil
}

func (p *BotProvisioner) Find(a *agent.Agent) (*agent.BotInfo, error) {
	user, err := p.api.GetUserByUsername("agent-" + a.ID)
	if err != nil {
		if err.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	if user == nil {
		return nil, nil
	}
	bot, ownErr := p.api.GetBot(user.Id, true)
	if ownErr != nil {
		if ownErr.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, ownErr
	}
	if bot.OwnerId != pluginID {
		return nil, nil
	}
	return &agent.BotInfo{UserID: user.Id, Username: user.Username}, nil
}
