// Package fileaccess enforces access to text attachments, independently of model arguments.
package fileaccess

import (
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/seyeon/agent-bridge/server/agent"
)

const MaxBytes = 2 * 1024 * 1024
const MaxOutputBytes = 20 * 1024

type Attachment struct {
	FileID   string `json:"file_id"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}
type Result struct {
	Attachment
	Truncated bool   `json:"truncated"`
	Content   string `json:"content"`
}
type Error struct {
	Code   string
	Status int
}

func (e *Error) Error() string              { return e.Code }
func failure(code string, status int) error { return &Error{code, status} }

type API interface {
	HasPermissionToChannel(string, string, *model.Permission) bool
	GetUser(string) (*model.User, *model.AppError)
	GetChannel(string) (*model.Channel, *model.AppError)
	GetChannelMember(string, string) (*model.ChannelMember, *model.AppError)
	GetFileInfo(string) (*model.FileInfo, *model.AppError)
	GetFile(string) ([]byte, *model.AppError)
}
type Service struct{ API API }

func ToolAllowed(a *agent.Agent) bool {
	if !a.Tools.Enabled || !a.Permissions.Files.Read {
		return false
	}
	allowed := false
	for _, id := range a.Tools.Allowed {
		if id == "file-reader" {
			allowed = true
		}
	}
	for _, id := range a.Tools.Denied {
		if id == "file-reader" {
			return false
		}
	}
	return allowed
}
func metadata(info *model.FileInfo) Attachment {
	return Attachment{info.Id, info.Name, info.MimeType, info.Size}
}

// Attachments includes metadata only, from the authoritative current post.
func (s Service) Attachments(post *model.Post) []Attachment {
	result := []Attachment{}
	for _, id := range post.FileIds {
		if len(result) >= 32 {
			break
		}
		info, err := s.API.GetFileInfo(id)
		if err == nil && info != nil && info.Id == id && info.PostId == post.Id && info.DeleteAt == 0 {
			result = append(result, metadata(info))
		}
	}
	return result
}
func supported(info *model.FileInfo) bool {
	ext := strings.ToLower(filepath.Ext(info.Name))
	switch ext {
	case ".txt", ".md", ".csv", ".json", ".yaml", ".yml", ".log":
	default:
		return false
	}
	mime := strings.ToLower(strings.TrimSpace(strings.Split(info.MimeType, ";")[0]))
	return strings.HasPrefix(mime, "text/") || mime == "application/json" || mime == "application/yaml" || mime == "application/x-yaml" || mime == "application/octet-stream"
}
func (s Service) Read(a *agent.Agent, post *model.Post, requester, channel, fileID string, limit int) (*Result, error) {
	if !ToolAllowed(a) || !a.Lifecycle.Enabled || a.Runtime.Status != "ACTIVE" || a.Messenger.UserID == nil || !a.Messenger.Bot {
		return nil, failure("FILE_PERMISSION_DENIED", http.StatusForbidden)
	}
	if post == nil || post.DeleteAt != 0 || post.UserId != requester || post.ChannelId != channel {
		return nil, failure("FILE_PERMISSION_DENIED", 403)
	}
	user, err := s.API.GetUser(requester)
	if err != nil || user == nil || user.DeleteAt != 0 || user.IsBot {
		return nil, failure("FILE_PERMISSION_DENIED", 403)
	}
	bot, err := s.API.GetUser(*a.Messenger.UserID)
	if err != nil || bot == nil || !bot.IsBot || bot.DeleteAt != 0 {
		return nil, failure("FILE_PERMISSION_DENIED", 403)
	}
	ch, err := s.API.GetChannel(channel)
	if err != nil || ch == nil || ch.DeleteAt != 0 {
		return nil, failure("FILE_PERMISSION_DENIED", 403)
	}
	if _, err = s.API.GetChannelMember(channel, requester); err != nil {
		return nil, failure("FILE_PERMISSION_DENIED", 403)
	}
	if _, err = s.API.GetChannelMember(channel, *a.Messenger.UserID); err != nil {
		return nil, failure("FILE_PERMISSION_DENIED", 403)
	}
	if !s.API.HasPermissionToChannel(requester, channel, model.PermissionReadChannel) || !s.API.HasPermissionToChannel(*a.Messenger.UserID, channel, model.PermissionReadChannel) {
		return nil, failure("FILE_PERMISSION_DENIED", 403)
	}
	// Check the supplied ID against the post before even probing file existence.
	attached := false
	for _, id := range post.FileIds {
		if id == fileID {
			attached = true
		}
	}
	if !attached {
		return nil, failure("FILE_NOT_ATTACHED", 403)
	}
	info, err := s.API.GetFileInfo(fileID)
	if err != nil || info == nil || info.DeleteAt != 0 {
		return nil, failure("FILE_NOT_FOUND", 404)
	}
	if info.Id != fileID || info.PostId != post.Id {
		return nil, failure("FILE_NOT_ATTACHED", 403)
	}
	if info.Size < 0 || info.Size > MaxBytes {
		return nil, failure("FILE_TOO_LARGE", 413)
	}
	if !supported(info) {
		return nil, failure("FILE_TYPE_NOT_SUPPORTED", 415)
	}
	data, err := s.API.GetFile(fileID)
	if err != nil {
		return nil, failure("FILE_READ_FAILED", 502)
	}
	if len(data) > MaxBytes {
		return nil, failure("FILE_TOO_LARGE", 413)
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return nil, failure("FILE_TYPE_NOT_SUPPORTED", 415)
	}
	if limit < 0 {
		limit = 0
	}
	if limit > MaxOutputBytes {
		limit = MaxOutputBytes
	}
	truncated := len(data) > limit
	if truncated {
		data = data[:limit]
		for len(data) > 0 && !utf8.Valid(data) {
			data = data[:len(data)-1]
		}
	}
	return &Result{Attachment: metadata(info), Truncated: truncated, Content: string(data)}, nil
}
