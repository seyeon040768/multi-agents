package mattermost

import (
	"context"
	"fmt"
	mm "github.com/mattermost/mattermost/server/public/model"
	"github.com/seyeon/agent-bridge/server/agent"
	"unicode/utf8"
)

type PostAPI interface {
	CreatePost(*mm.Post) (*mm.Post, *mm.AppError)
}
type Messenger struct{ API PostAPI }

func (m *Messenger) Reply(ctx context.Context, a *agent.Agent, p *mm.Post, text string, isError bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.Messenger.UserID == nil {
		return fmt.Errorf("agent has no Bot")
	}
	// Keep under Mattermost's default 16,383-character limit. Full streaming is out of scope.
	runes := []rune(text)
	if len(runes) > 16000 {
		text = string(runes[:15950]) + "\n\n[응답 길이 제한으로 일부가 생략되었습니다.]"
	}
	if !utf8.ValidString(text) {
		return fmt.Errorf("invalid reply text")
	}
	root := p.RootId
	if root == "" {
		root = p.Id
	}
	props := mm.StringInterface{"agentbridge_generated": true}
	if isError {
		props["agentbridge_error"] = true
	}
	_, err := m.API.CreatePost(&mm.Post{UserId: *a.Messenger.UserID, ChannelId: p.ChannelId, RootId: root, Message: text, Props: props})
	if err != nil {
		return err
	}
	return nil
}
