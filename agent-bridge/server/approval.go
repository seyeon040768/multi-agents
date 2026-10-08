package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"
	"github.com/seyeon/agent-bridge/server/agent"
	"github.com/seyeon/agent-bridge/server/messenger/mattermost"
	"github.com/seyeon/agent-bridge/server/modelclient"
)

var approvalIDPattern = regexp.MustCompile(`^apr_[a-f0-9]{32}$`)

const approvalPrefix = "approval:v1:"

type pendingApproval struct {
	ID           string                     `json:"id"`
	AgentID      string                     `json:"agent_id"`
	RootPostID   string                     `json:"root_post_id"`
	SourcePostID string                     `json:"source_post_id"`
	ChannelID    string                     `json:"channel_id"`
	PostID       string                     `json:"post_id"`
	RequestedBy  string                     `json:"requested_by"`
	DecidedBy    string                     `json:"decided_by,omitempty"`
	Status       string                     `json:"status"`
	Decision     string                     `json:"decision,omitempty"`
	CreatedAt    time.Time                  `json:"created_at"`
	ExpiresAt    time.Time                  `json:"expires_at"`
	DecidedAt    *time.Time                 `json:"decided_at,omitempty"`
	FinishedAt   *time.Time                 `json:"finished_at,omitempty"`
	Calls        []modelclient.ApprovalCall `json:"calls"`
}

// Request stores the immutable tool call details before publishing any buttons.
func (p *Plugin) Request(ctx context.Context, a *agent.Agent, source *model.Post, in *modelclient.ApprovalInterrupt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if in == nil || !approvalIDPattern.MatchString(in.ApprovalID) || len(in.Calls) == 0 || len(in.Calls) > 10 || in.ExpiresAt.Before(in.CreatedAt) {
		return fmt.Errorf("invalid approval")
	}
	root := source.RootId
	if root == "" {
		root = source.Id
	}
	item := &pendingApproval{ID: in.ApprovalID, AgentID: a.ID, RootPostID: root, SourcePostID: source.Id, ChannelID: source.ChannelId, RequestedBy: source.UserId, Status: "PENDING", CreatedAt: in.CreatedAt, ExpiresAt: in.ExpiresAt, Calls: in.Calls}
	if a.Messenger.UserID == nil {
		return fmt.Errorf("agent has no Bot")
	}
	text := "⚠️ 도구 실행 승인이 필요합니다.\n\n"
	for _, call := range in.Calls {
		// JSON string newlines are escaped, so argument values cannot close the fenced block.
		args := string(call.Args)
		if len(args) > 8000 { // Never ask users to approve partially displayed arguments.
			return fmt.Errorf("approval arguments too large")
		}
		if call.ToolID == "file-reader" && call.Name == "read_file" {
			var args struct {
				FileID string `json:"file_id"`
			}
			if json.Unmarshal(call.Args, &args) == nil {
				for _, id := range source.FileIds {
					if id != args.FileID {
						continue
					}
					info, e := p.API.GetFileInfo(id)
					if e == nil && info != nil && info.PostId == source.Id && info.DeleteAt == 0 {
						// JSON escaping prevents filenames from injecting Markdown.
						label, _ := json.Marshal(map[string]string{"file_name": info.Name})
						text += "현재 첨부 파일을 읽으려고 합니다.\n```json\n" + string(label) + "\n```\n"
					}
					break
				}
			}
		}
		text += "Tool: **" + call.ToolID + "**\n```json\n" + args + "\n```\n"
	}
	if len([]rune(text)) > 14000 {
		return fmt.Errorf("approval display too large")
	}
	text += "\n요청자 또는 시스템 관리자만 결정할 수 있습니다. 30분 후 만료됩니다."
	raw, err := json.Marshal(item)
	if err != nil {
		return err
	}
	ok, appErr := p.API.KVCompareAndSet(approvalPrefix+item.ID, nil, raw)
	if appErr != nil {
		return appErr
	}
	if !ok {
		return nil
	} // repeated interrupt delivery must not publish duplicate buttons

	actions := []*model.PostAction{}
	for _, decision := range []string{"approve", "reject"} {
		name := "승인"
		if decision == "reject" {
			name = "거절"
		}
		actions = append(actions, &model.PostAction{Id: decision, Name: name, Type: "button", Integration: &model.PostActionIntegration{URL: "/plugins/com.seyeon.agentbridge/api/v1/approvals/" + item.ID + "/" + decision, Context: map[string]any{"approval_id": item.ID}}})
	}
	// Mattermost 11.7's shipped SDK/server supports attachment actions. Blocks require a newer server.
	post, appErr := p.API.CreatePost(&model.Post{UserId: *a.Messenger.UserID, ChannelId: item.ChannelID, RootId: root, Message: text, Props: model.StringInterface{"agentbridge_generated": true, "attachments": []*model.SlackAttachment{{Actions: actions}}}})
	if appErr != nil {
		return appErr
	}
	item.PostID = post.Id
	next, _ := json.Marshal(item)
	ok, appErr = p.API.KVCompareAndSet(approvalPrefix+item.ID, raw, next)
	if appErr != nil {
		return appErr
	}
	if !ok {
		return fmt.Errorf("approval publication conflict")
	}
	p.auditApproval(item, "requested")
	return nil
}

func (p *Plugin) handleApproval(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !approvalIDPattern.MatchString(id) {
		apiError(w, 400, "INVALID_APPROVAL", "invalid approval")
		return
	}
	var body model.PostActionIntegrationRequest
	if !decodeBody(w, r, &body) {
		return
	}
	actor := r.Header.Get("Mattermost-User-ID")
	if body.UserId != actor {
		apiError(w, 403, "FORBIDDEN", "authenticated actor mismatch")
		return
	}
	raw, err := p.API.KVGet(approvalPrefix + id)
	if err != nil {
		apiError(w, 500, "INTERNAL_ERROR", "approval lookup failed")
		return
	}
	var item pendingApproval
	if len(raw) == 0 || json.Unmarshal(raw, &item) != nil {
		apiError(w, 404, "APPROVAL_NOT_FOUND", "approval not found")
		return
	}
	if body.PostId != item.PostID || body.ChannelId != item.ChannelID || item.PostID == "" {
		apiError(w, 403, "FORBIDDEN", "approval post mismatch")
		return
	}
	user, err := p.API.GetUser(actor)
	if err != nil || user.DeleteAt != 0 {
		apiError(w, 403, "FORBIDDEN", "user unavailable")
		return
	}
	admin := false
	for _, role := range strings.Fields(user.Roles) {
		if role == "system_admin" {
			admin = true
		}
	}
	if actor != item.RequestedBy && !admin {
		apiError(w, 403, "FORBIDDEN", "only requester or system administrator can decide")
		return
	}
	if _, err = p.API.GetChannelMember(item.ChannelID, actor); err != nil {
		apiError(w, 403, "FORBIDDEN", "channel membership required")
		return
	}
	if item.Status != "PENDING" {
		apiError(w, 409, "APPROVAL_ALREADY_PROCESSED", "already processed")
		return
	}
	item.Decision = mux.Vars(r)["decision"]
	item.Status = "APPROVED"
	if item.Decision == "reject" {
		item.Status = "REJECTED"
	}
	now := time.Now().UTC()
	if !now.Before(item.ExpiresAt) {
		item.Decision = "expire"
		item.Status = "EXPIRED"
	}
	item.DecidedBy = actor
	item.DecidedAt = &now
	if !p.queueApproval(raw, &item) {
		apiError(w, 409, "APPROVAL_CONFLICT", "already processed or busy; retry")
		return
	}
	// Async processing avoids Mattermost's short action callback timeout.
	writeJSON(w, 200, map[string]any{"ephemeral_text": "승인 결정을 접수했습니다. 처리 결과는 스레드에 표시됩니다."})
}

func (p *Plugin) queueApproval(old []byte, item *pendingApproval) bool {
	next, _ := json.Marshal(item)
	ok, err := p.API.KVCompareAndSet(approvalPrefix+item.ID, old, next)
	if err != nil || !ok {
		return false
	}
	select {
	case p.approvalQueue <- item:
		p.auditApproval(item, "decided")
		return true
	default:
		// No worker has received this item; rollback is safe and allows a later click.
		_, _ = p.API.KVCompareAndSet(approvalPrefix+item.ID, next, old)
		return false
	}
}

func (p *Plugin) processApproval(item *pendingApproval) {
	ctx, cancel := context.WithTimeout(p.chatCtx, 90*time.Second)
	defer cancel()
	mutex, lockErr := cluster.NewMutex(p.API, "agent-approval:v1:"+item.ID)
	if lockErr != nil {
		return
	}
	if lockErr = mutex.LockWithContext(ctx); lockErr != nil {
		return
	}
	defer mutex.Unlock()
	raw, kvErr := p.API.KVGet(approvalPrefix + item.ID)
	var authoritative pendingApproval
	if kvErr != nil || json.Unmarshal(raw, &authoritative) != nil || authoritative.FinishedAt != nil || authoritative.Status != item.Status || authoritative.Decision != item.Decision {
		return
	}
	item = &authoritative
	p.updateApprovalPost(item, "승인 결정을 접수했습니다. 처리 중입니다.")
	a, err := p.agents.Get(item.AgentID)
	if err != nil {
		p.finishApproval(item, "FAILED")
		return
	}
	tools := a.Tools
	if !a.Lifecycle.Enabled || a.Runtime.Status != "ACTIVE" {
		tools.Enabled = false
	}
	cfg := p.getConfiguration()
	result, err := (&modelclient.LangGraph{URL: cfg.LangGraphURL, Token: cfg.LangGraphToken}).Resume(ctx, modelclient.ResumeRequest{AgentID: item.AgentID, RootPostID: item.RootPostID, ApprovalID: item.ID, Decision: item.Decision, Tools: tools, Permissions: a.Permissions})
	if err != nil {
		p.finishApproval(item, "FAILED")
		return
	}
	status := item.Status
	if status == "APPROVED" {
		status = "EXECUTED"
	}
	// Check tool outcomes, rather than claiming execution merely because the model answered.
	if result.ApprovalStatus != "" {
		status = result.ApprovalStatus
	} else {
		status = "FAILED"
	}
	p.finishApproval(item, status)
	source := &model.Post{Id: item.SourcePostID, RootId: item.RootPostID, ChannelId: item.ChannelID, UserId: item.RequestedBy}
	if result.Status == "interrupted" {
		if err := p.Request(ctx, a, source, result.Interrupt); err != nil {
			p.API.LogError("Approval publication failed", "approval_id", item.ID)
		}
		return
	}
	current, err := p.agents.Get(item.AgentID)
	if err != nil || !current.Lifecycle.Enabled || current.Runtime.Status != "ACTIVE" {
		return
	}
	if err := (&mattermost.Messenger{API: p.API}).Reply(ctx, current, source, result.Text, false); err != nil {
		p.API.LogError("Approval reply failed", "approval_id", item.ID)
	}
}

func (p *Plugin) finishApproval(item *pendingApproval, status string) {
	old, err := p.API.KVGet(approvalPrefix + item.ID)
	if err != nil {
		return
	}
	var stored pendingApproval
	if json.Unmarshal(old, &stored) != nil || stored.Status != item.Status {
		return
	}
	now := time.Now().UTC()
	stored.Status = status
	stored.FinishedAt = &now
	next, _ := json.Marshal(stored)
	ok, err := p.API.KVCompareAndSet(approvalPrefix+item.ID, old, next)
	if err != nil || !ok {
		return
	}
	p.updateApprovalPost(&stored, map[string]string{"EXECUTED": "✅ 승인한 도구 실행이 완료되었습니다.", "REJECTED": "❌ 거절됨: 도구를 실행하지 않았습니다.", "EXPIRED": "⌛ 만료됨: 도구를 실행하지 않았습니다.", "FAILED": "⚠️ 처리 실패: 실행 여부를 확인할 수 없거나 도구 실행이 실패했습니다. 자동으로 재실행하지 않습니다."}[status])
	p.auditApproval(&stored, "finished")
}
func (p *Plugin) updateApprovalPost(item *pendingApproval, text string) {
	if item.PostID == "" {
		return
	}
	post, err := p.API.GetPost(item.PostID)
	if err != nil {
		return
	}
	post.Message = text
	post.DelProp("attachments")
	if _, err := p.API.UpdatePost(post); err != nil {
		p.API.LogError("Approval post update failed", "approval_id", item.ID)
	}
}
func (p *Plugin) auditApproval(item *pendingApproval, event string) {
	p.API.LogInfo("Agent tool approval", "event", event, "approval_id", item.ID, "requested_by", item.RequestedBy, "decided_by", item.DecidedBy, "agent_id", item.AgentID, "status", item.Status, "created_at", item.CreatedAt.String(), "decided_at", item.DecidedAt, "finished_at", item.FinishedAt)
	for _, call := range item.Calls {
		p.API.LogInfo("Agent tool approval call", "approval_id", item.ID, "tool_id", call.ToolID, "tool_call_id", call.ToolCallID)
	}
}
func (p *Plugin) expireApprovals() {
	for page := 0; ; page++ {
		keys, err := p.API.KVList(page, 100)
		if err != nil {
			return
		}
		for _, key := range keys {
			if !strings.HasPrefix(key, approvalPrefix) {
				continue
			}
			raw, err := p.API.KVGet(key)
			if err != nil {
				continue
			}
			var item pendingApproval
			if json.Unmarshal(raw, &item) != nil || item.Status != "PENDING" || time.Now().Before(item.ExpiresAt) {
				continue
			}
			now := time.Now().UTC()
			item.Decision = "expire"
			item.Status = "EXPIRED"
			item.DecidedAt = &now
			p.queueApproval(raw, &item)
		}
		if len(keys) < 100 {
			return
		}
	}
}

// Recover durable decisions only once on activation. Runtime accepts a resume only while
// the exact interrupt is still pending; it never re-executes a consumed tool node.
func (p *Plugin) recoverApprovalDecisions() {
	if p.API == nil || p.chatCtx.Err() != nil {
		return
	}
	for page := 0; ; page++ {
		keys, err := p.API.KVList(page, 100)
		if err != nil {
			return
		}
		for _, key := range keys {
			if !strings.HasPrefix(key, approvalPrefix) {
				continue
			}
			raw, err := p.API.KVGet(key)
			if err != nil {
				continue
			}
			var item pendingApproval
			if json.Unmarshal(raw, &item) != nil || item.FinishedAt != nil || item.Decision == "" {
				continue
			}
			if item.Status != "APPROVED" && item.Status != "REJECTED" && item.Status != "EXPIRED" {
				continue
			}
			select {
			case <-p.chatCtx.Done():
				return
			case p.approvalQueue <- &item:
			}
		}
		if len(keys) < 100 {
			return
		}
	}
}
