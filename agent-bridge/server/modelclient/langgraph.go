package modelclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LangGraph calls the provider-neutral runtime; no provider credentials enter Agent KV.
type LangGraph struct {
	URL, Token string
	HTTP       *http.Client
}

func (c *LangGraph) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	return c.call(ctx, "/v1/generate", req)
}
func (c *LangGraph) Resume(ctx context.Context, req ResumeRequest) (*GenerateResponse, error) {
	return c.call(ctx, "/v1/resume", req)
}
func (c *LangGraph) call(ctx context.Context, path string, req interface{}) (*GenerateResponse, error) {
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid LangGraph runtime URL")
	}
	if c.Token == "" {
		return nil, fmt.Errorf("LangGraph runtime authentication is not configured")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+c.Token)
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, err := client.Do(r)
	if err != nil {
		return nil, fmt.Errorf("runtime transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("runtime returned HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024+1))
	if err != nil || len(raw) > 256*1024 {
		return nil, fmt.Errorf("invalid runtime response size")
	}
	var result GenerateResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("invalid runtime response JSON")
	}
	if result.Status != "" && result.Status != "completed" && result.Status != "interrupted" {
		return nil, fmt.Errorf("invalid runtime status")
	}
	switch result.ApprovalStatus {
	case "", "EXECUTED", "REJECTED", "EXPIRED", "FAILED":
	default:
		return nil, fmt.Errorf("invalid approval outcome")
	}
	if result.Status == "interrupted" {
		if result.Interrupt == nil || result.Interrupt.Type != "tool_approval" || len(result.Interrupt.Calls) == 0 {
			return nil, fmt.Errorf("invalid runtime interrupt")
		}
		return &result, nil
	}
	if strings.TrimSpace(result.Text) == "" {
		return nil, fmt.Errorf("runtime returned empty text")
	}
	return &result, nil
}
