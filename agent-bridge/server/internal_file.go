package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/seyeon/agent-bridge/server/fileaccess"
	"github.com/seyeon/agent-bridge/server/orchestrator"
)

type internalFileRequest struct {
	AgentID         string `json:"agent_id"`
	RequesterUserID string `json:"requester_user_id"`
	ChannelID       string `json:"channel_id"`
	PostID          string `json:"post_id"`
	FileID          string `json:"file_id"`
	MaxContentBytes int    `json:"max_content_bytes"`
	ApprovalID      string `json:"approval_id,omitempty"`
}

func (p *Plugin) fileApprovalValid(a *agent.Agent, in internalFileRequest) bool {
	required := false
	for _, id := range a.Tools.RequireConfirmation {
		if id == "file-reader" {
			required = true
		}
	}
	if !required {
		return true
	}
	if !approvalIDPattern.MatchString(in.ApprovalID) {
		return false
	}
	raw, err := p.API.KVGet(approvalPrefix + in.ApprovalID)
	var saved pendingApproval
	if err != nil || json.Unmarshal(raw, &saved) != nil || saved.ID != in.ApprovalID || saved.AgentID != in.AgentID || saved.SourcePostID != in.PostID || saved.ChannelID != in.ChannelID || saved.RequestedBy != in.RequesterUserID || saved.Decision != "approve" || saved.Status != "APPROVED" || saved.FinishedAt != nil || time.Now().After(saved.ExpiresAt) {
		return false
	}
	for _, call := range saved.Calls {
		var args struct {
			FileID string `json:"file_id"`
		}
		if call.ToolID == "file-reader" && call.Name == "read_file" && json.Unmarshal(call.Args, &args) == nil && args.FileID == in.FileID {
			return true
		}
	}
	return false
}
func (p *Plugin) handleInternalFile(w http.ResponseWriter, r *http.Request) {
	secret := p.getConfiguration().LangGraphToken
	if secret == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+secret)) != 1 {
		apiError(w, 401, "UNAUTHORIZED", "runtime authentication required")
		return
	}
	var in internalFileRequest
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if !decodeBody(w, r, &in) {
		return
	}
	if !model.IsValidId(in.RequesterUserID) || !model.IsValidId(in.ChannelID) || !model.IsValidId(in.PostID) || !model.IsValidId(in.FileID) || in.MaxContentBytes < 0 || in.MaxContentBytes > fileaccess.MaxOutputBytes {
		apiError(w, 400, "FILE_INVALID_REQUEST", "invalid file request")
		return
	}
	a, err := p.agents.Get(in.AgentID)
	if err != nil || !fileaccess.ToolAllowed(a) {
		apiError(w, 403, "FILE_PERMISSION_DENIED", "file access denied")
		return
	}
	post, appErr := p.API.GetPost(in.PostID)
	if appErr != nil || post == nil {
		apiError(w, 404, "FILE_NOT_FOUND", "file request not found")
		return
	}
	resolved, err := (&orchestrator.Resolver{API: p.API, Agents: agent.NewKVAgentStore(p.API)}).Resolve(post)
	if err != nil || resolved == nil || resolved.ID != a.ID || !p.fileApprovalValid(a, in) {
		apiError(w, 403, "FILE_PERMISSION_DENIED", "file access denied")
		return
	}
	result, err := (fileaccess.Service{API: p.API}).Read(a, post, in.RequesterUserID, in.ChannelID, in.FileID, in.MaxContentBytes)
	if err != nil {
		var safe *fileaccess.Error
		if errors.As(err, &safe) {
			apiError(w, safe.Status, safe.Code, "file reader could not read this attachment")
			return
		}
		apiError(w, 502, "FILE_READ_FAILED", "file reader failed")
		return
	}
	writeJSON(w, 200, result)
}
