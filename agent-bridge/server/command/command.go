package command

import (
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

const agentCommandTrigger = "agent"

type Handler struct {
	client *pluginapi.Client
}

type Command interface {
	Handle(args *model.CommandArgs) (*model.CommandResponse, error)
}

// /agent 명령어 등록
func NewCommandHandler(client *pluginapi.Client) Command {
	err := client.SlashCommand.Register(&model.Command{
		Trigger:          agentCommandTrigger,
		AutoComplete:     true,
		AutoCompleteDesc: "Manage AI agents",
		AutoCompleteHint: "create | list",
	})
	if err != nil {
		client.Log.Error("Failed to register /agent command", "error", err)
	}

	return &Handler{
		client: client,
	}
}

func (c *Handler) Handle(args *model.CommandArgs) (*model.CommandResponse, error) {
	return ephemeral("UI를 불러온 후 `/agent create` 또는 `/agent list`를 실행해주세요. 이 버전은 UI 미리보기이며 Bot이나 설정을 저장하지 않습니다."), nil
}

func ephemeral(text string) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         text,
	}
}
