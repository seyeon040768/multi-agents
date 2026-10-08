package modelclient

import (
	"context"
	"encoding/json"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/seyeon/agent-bridge/server/fileaccess"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeHTTPContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/generate", r.URL.Path)
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		var req GenerateRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.NotNil(t, req.MaxContextTokens)
		require.EqualValues(t, 12000, *req.MaxContextTokens)
		require.Equal(t, "researcher", req.AgentID)
		require.Equal(t, "root", req.RootPostID)
		require.Equal(t, "post", req.PostID)
		require.True(t, req.Permissions.Files.Read)
		require.Equal(t, "requester", req.RequesterUserID)
		require.Equal(t, "channel", req.ChannelID)
		require.Len(t, req.Attachments, 1)
		require.Equal(t, "file", req.Attachments[0].FileID)
		require.Equal(t, "anthropic", req.Provider)
		require.Equal(t, "claude-sonnet-4-6", req.Model)
		require.Equal(t, agent.AgentTools{Enabled: true, Allowed: []string{"debug-echo"}, Denied: []string{"web-search"}, RequireConfirmation: []string{"debug-echo"}}, req.Tools)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"answer","input_tokens":4,"output_tokens":2}`))
	}))
	defer server.Close()
	client := &LangGraph{URL: server.URL, Token: "test-token"}
	budget := int64(12000)
	resp, err := client.Generate(context.Background(), GenerateRequest{RequesterUserID: "requester", ChannelID: "channel", Attachments: []fileaccess.Attachment{{FileID: "file", Name: "project.md", MimeType: "text/markdown", Size: 120}}, Permissions: agent.AgentPermissions{Files: agent.AgentPermissionsFiles{Read: true}}, MaxContextTokens: &budget, Tools: agent.AgentTools{Enabled: true, Allowed: []string{"debug-echo"}, Denied: []string{"web-search"}, RequireConfirmation: []string{"debug-echo"}}, AgentID: "researcher", RootPostID: "root", PostID: "post", Provider: "anthropic", Model: "claude-sonnet-4-6"})
	require.NoError(t, err)
	require.Equal(t, "answer", resp.Text)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Generate(ctx, GenerateRequest{})
	require.Error(t, err)
}
func TestRuntimeErrorsDoNotExposeBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "secret-api-key", 401) }))
	defer server.Close()
	_, err := (&LangGraph{URL: server.URL, Token: "token"}).Generate(context.Background(), GenerateRequest{})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret-api-key")
}
