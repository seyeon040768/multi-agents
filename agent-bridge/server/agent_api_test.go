package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/stretchr/testify/require"
)

type testKV struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (f *testKV) KVGet(key string) ([]byte, *model.AppError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.values[key]...), nil
}
func (f *testKV) KVCompareAndSet(key string, old, next []byte) (bool, *model.AppError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !bytes.Equal(f.values[key], old) {
		return false, nil
	}
	f.values[key] = append([]byte(nil), next...)
	return true, nil
}
func (f *testKV) KVCompareAndDelete(key string, old []byte) (bool, *model.AppError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.values[key] == nil || !bytes.Equal(f.values[key], old) {
		return false, nil
	}
	delete(f.values, key)
	return true, nil
}
func TestAgentHTTP(t *testing.T) {
	api := &plugintest.API{}
	api.On("GetUser", "admin").Return(&model.User{Id: "admin", Roles: "system_user system_admin"}, nil)
	api.On("GetUser", "user").Return(&model.User{Id: "user", Roles: "system_user"}, nil)
	p := &Plugin{}
	p.API = api
	p.agents = agent.NewService(agent.NewKVAgentStore(&testKV{values: map[string][]byte{}}))
	p.router = p.initRouter()
	request := func(method, path, user string, body interface{}, status int) *httptest.ResponseRecorder {
		t.Helper()
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(raw))
		if user != "" {
			r.Header.Set("Mattermost-User-ID", user)
		}
		w := httptest.NewRecorder()
		p.ServeHTTP(nil, w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		if status >= 400 {
			var payload struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
			require.NotEmpty(t, payload.Error.Code)
		}
		return w
	}
	a := agent.DefaultAgent()
	a.ID = "researcher"
	a.Name = "Researcher"
	a.Model.Name = "gpt-5.4"
	a.Prompts.Identity = "Research carefully."
	request("GET", "/agents", "", nil, 401)
	for _, tc := range []struct{ method, path string }{{"POST", "/agents"}, {"PUT", "/agents/researcher"}, {"DELETE", "/agents/researcher?version=1"}, {"POST", "/agents/researcher/enable"}, {"POST", "/agents/researcher/disable"}} {
		request(tc.method, tc.path, "user", a, 403)
	}
	created := request("POST", "/agents", "admin", a, 201)
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &a))
	require.EqualValues(t, 1, a.Lifecycle.Version)
	request("POST", "/agents", "admin", a, 409)
	request("GET", "/agents", "user", nil, 200)
	request("GET", "/agents/researcher", "user", nil, 200)
	a.Name = "Updated"
	updated := request("PUT", "/agents/researcher", "admin", a, 200)
	request("PUT", "/agents/researcher", "admin", a, 409)
	require.NoError(t, json.Unmarshal(updated.Body.Bytes(), &a))
	require.EqualValues(t, 2, a.Lifecycle.Version)
	request("POST", "/agents/researcher/disable", "admin", map[string]int{"version": 2}, 200)
	request("POST", "/agents/researcher/enable", "admin", map[string]int{"version": 2}, 409)
	request("POST", "/agents/researcher/enable", "admin", map[string]int{"version": 3}, 200)
	request("DELETE", "/agents/researcher?version=3", "admin", nil, 409)
	request("DELETE", "/agents/researcher?version=4", "admin", nil, 204)
	request("GET", "/agents/researcher", "user", nil, 404)
	listed := request("GET", "/agents", "user", nil, 200)
	require.JSONEq(t, "[]", listed.Body.String())
	request("DELETE", "/agents/researcher", "admin", nil, 400)
	for _, tc := range []struct {
		name   string
		change func(*agent.Agent)
	}{
		{"id", func(a *agent.Agent) { a.ID = "BAD" }}, {"name", func(a *agent.Agent) { a.Name = " " }}, {"prompt", func(a *agent.Agent) { a.Prompts.Identity = " " }}, {"model", func(a *agent.Agent) { a.Model.Name = "unknown" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := a
			tc.change(&invalid)
			request("POST", "/agents", "admin", invalid, 400)
		})
	}
	request("POST", "/agents", "admin", map[string]interface{}{"id": "researcher", "name": "Researcher", "model": map[string]interface{}{"provider": "openai", "name": "gpt-5.4", "api_key": "secret"}, "prompts": map[string]string{"identity": "Research."}}, 400)
	request("POST", "/agents", "admin", map[string]interface{}{"id": 5}, 400)
	request("GET", "/agents/BAD", "user", nil, 400)
	request("POST", "/agents/researcher/enable", "admin", map[string]int{"version": 0}, 400)
	// Minimal valid JSON gets server defaults, and unknown credential fields are rejected.
	request("POST", "/agents", "admin", map[string]interface{}{"id": "minimal", "name": "Minimal", "model": map[string]string{"provider": "openai", "name": "gpt-5.4"}, "prompts": map[string]string{"identity": "Research."}}, 201)
	request("PUT", "/agents/minimal", "admin", a, 400)
	request("PUT", "/agents/minimal", "admin", map[string]interface{}{"id": "minimal", "name": "Minimal", "model": map[string]string{"provider": "openai", "name": "gpt-5.4"}, "prompts": map[string]string{"identity": "Research."}}, 400)
}
func TestInvalidAgentJSON(t *testing.T) {
	for _, body := range []string{`{`, `null`, `{} {}`, `{"id":"researcher","lifecycle":{"version":"1"}}`} {
		t.Run(body, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
			a := agent.DefaultAgent()
			if body == "null" { // Null is syntactically JSON but must not count as an object.
				require.False(t, decodeBody(w, r, &a))
				return
			}
			require.False(t, decodeBody(w, r, &a))
		})
	}
}
