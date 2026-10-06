package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/stretchr/testify/require"
)

type apiBots struct {
	fail  bool
	calls int
}

func (b *apiBots) Create(a *agent.Agent) (*agent.BotInfo, error) {
	b.calls++
	if b.fail {
		return nil, errors.New("creation failed")
	}
	return &agent.BotInfo{UserID: "bot-id", Username: "agent-" + a.ID}, nil
}
func (*apiBots) Update(*agent.Agent) (*agent.BotInfo, error) { return nil, nil }
func (*apiBots) SetActive(string, bool) error                { return nil }
func (*apiBots) Delete(string) error                         { return nil }
func TestBotProvisioningHTTP(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "error"}[fail], func(t *testing.T) {
			api := &plugintest.API{}
			api.On("GetUser", "admin").Return(&model.User{Roles: "system_admin"}, (*model.AppError)(nil))
			api.On("GetUser", "user").Return(&model.User{Roles: "system_user"}, (*model.AppError)(nil))
			bots := &apiBots{fail: fail}
			p := &Plugin{}
			p.API = api
			p.agents = agent.NewServiceWithBots(agent.NewKVAgentStore(&testKV{values: map[string][]byte{}}), bots, nil)
			p.router = p.initRouter()
			a := agent.DefaultAgent()
			a.ID = "researcher"
			a.Name = "Researcher"
			a.Model.Name = "gpt-5.4"
			a.Prompts.Identity = "Research"
			a.Runtime.Status = "forged"
			raw, err := json.Marshal(a)
			require.NoError(t, err)
			request := func(user string) *httptest.ResponseRecorder {
				r := httptest.NewRequest("POST", "/api/v1/agents", bytes.NewReader(raw))
				r.Header.Set("Mattermost-User-ID", user)
				w := httptest.NewRecorder()
				p.ServeHTTP(nil, w, r)
				return w
			}
			require.Equal(t, 403, request("user").Code)
			require.Zero(t, bots.calls)
			response := request("admin")
			require.Equal(t, 201, response.Code, response.Body.String())
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &a))
			if fail {
				require.Equal(t, "ERROR", a.Runtime.Status)
				require.NotNil(t, a.Runtime.Error)
			} else {
				require.Equal(t, "ACTIVE", a.Runtime.Status)
				require.Equal(t, "bot-id", *a.Messenger.UserID)
			}
			require.Equal(t, 409, request("admin").Code)
			require.Equal(t, 1, bots.calls)
		})
	}
}
