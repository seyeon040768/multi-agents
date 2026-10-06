package modelclient

import (
	"context"
	"encoding/json"
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
		require.Equal(t, "anthropic", req.Provider)
		require.Equal(t, "claude-sonnet-4-6", req.Model)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"answer","input_tokens":4,"output_tokens":2}`))
	}))
	defer server.Close()
	client := &LangGraph{URL: server.URL, Token: "test-token"}
	resp, err := client.Generate(context.Background(), GenerateRequest{Provider: "anthropic", Model: "claude-sonnet-4-6"})
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
