package modelclient

import (
	"context"
	"encoding/json"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/seyeon/agent-bridge/server/fileaccess"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type GenerateRequest struct {
	Permissions      agent.AgentPermissions  `json:"permissions"`
	RequesterUserID  string                  `json:"requester_user_id"`
	ChannelID        string                  `json:"channel_id"`
	Attachments      []fileaccess.Attachment `json:"attachments"`
	MaxContextTokens *int64                  `json:"max_context_tokens"`
	Tools            agent.AgentTools        `json:"tools"`
	AgentID          string                  `json:"agent_id"`
	RootPostID       string                  `json:"root_post_id"`
	PostID           string                  `json:"post_id"`
	Provider         string                  `json:"provider"`
	Model            string                  `json:"model"`
	Messages         []Message               `json:"messages"`
	Temperature      float64                 `json:"temperature"`
	MaxTokens        int64                   `json:"max_tokens"`
	TopP             *float64                `json:"top_p"`
}
type ApprovalCall struct {
	ToolID     string          `json:"tool_id"`
	Name       string          `json:"name"`
	Args       json.RawMessage `json:"args"`
	ToolCallID string          `json:"tool_call_id"`
}
type ApprovalInterrupt struct {
	Type       string         `json:"type"`
	ApprovalID string         `json:"approval_id"`
	Calls      []ApprovalCall `json:"calls"`
	CreatedAt  time.Time      `json:"created_at"`
	ExpiresAt  time.Time      `json:"expires_at"`
}
type ResumeRequest struct {
	Permissions agent.AgentPermissions `json:"permissions"`
	AgentID     string                 `json:"agent_id"`
	RootPostID  string                 `json:"root_post_id"`
	ApprovalID  string                 `json:"approval_id"`
	Decision    string                 `json:"decision"`
	Tools       agent.AgentTools       `json:"tools"`
}
type GenerateResponse struct {
	Status         string             `json:"status,omitempty"`
	ApprovalStatus string             `json:"approval_status,omitempty"`
	Interrupt      *ApprovalInterrupt `json:"interrupt,omitempty"`
	Text           string             `json:"text"`
	InputTokens    int64              `json:"input_tokens"`
	OutputTokens   int64              `json:"output_tokens"`
}
type Client interface {
	Generate(context.Context, GenerateRequest) (*GenerateResponse, error)
}
