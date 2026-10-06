package modelclient

import "context"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type GenerateRequest struct {
	Provider    string    `json:"provider"`
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int64     `json:"max_tokens"`
	TopP        *float64  `json:"top_p"`
}
type GenerateResponse struct {
	Text         string `json:"text"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
}
type Client interface {
	Generate(context.Context, GenerateRequest) (*GenerateResponse, error)
}
