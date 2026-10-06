package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/seyeon/agent-bridge/server/agent"
)

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(w).Encode(value)
	}
}
func apiError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]interface{}{"error": map[string]string{"code": code, "message": message}})
}
func (p *Plugin) agentError(w http.ResponseWriter, err error) {
	var v *agent.ValidationError
	switch {
	case errors.As(err, &v):
		apiError(w, 400, v.Code, v.Message)
	case errors.Is(err, agent.ErrNotFound):
		apiError(w, 404, "AGENT_NOT_FOUND", err.Error())
	case errors.Is(err, agent.ErrAlreadyExists):
		apiError(w, 409, "AGENT_ALREADY_EXISTS", err.Error())
	case errors.Is(err, agent.ErrConflict):
		apiError(w, 409, "AGENT_VERSION_CONFLICT", err.Error())
	default:
		p.API.LogError("Agent management failed", "error", err.Error())
		apiError(w, 500, "INTERNAL_ERROR", "agent management failed")
	}
}
func decodeBody(w http.ResponseWriter, r *http.Request, value interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024)
	raw, err := io.ReadAll(r.Body)
	if err != nil || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		apiError(w, 400, "INVALID_JSON", "expected a JSON object")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		apiError(w, 400, "INVALID_JSON", "invalid request JSON")
		return false
	}
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		apiError(w, 400, "INVALID_JSON", "expected a single JSON object")
		return false
	}
	return true
}
func (p *Plugin) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		user, err := p.API.GetUser(r.Header.Get("Mattermost-User-ID"))
		if err != nil {
			p.agentError(w, err)
			return
		}
		admin := false
		for _, role := range strings.Fields(user.Roles) {
			if role == "system_admin" {
				admin = true
			}
		}
		if !admin {
			apiError(w, 403, "FORBIDDEN", "system administrator permission is required")
			return
		}
	}
	id := mux.Vars(r)["id"]
	action := mux.Vars(r)["action"]
	switch r.Method {
	case http.MethodGet:
		if id == "" {
			items, err := p.agents.List()
			if err != nil {
				p.agentError(w, err)
				return
			}
			writeJSON(w, 200, items)
		} else {
			a, err := p.agents.Get(id)
			if err != nil {
				p.agentError(w, err)
				return
			}
			writeJSON(w, 200, a)
		}
	case http.MethodPost, http.MethodPut:
		if action != "" {
			var body struct {
				Version int64 `json:"version"`
			}
			if !decodeBody(w, r, &body) {
				return
			}
			if body.Version < 1 {
				apiError(w, 400, "INVALID_VERSION", "positive version is required")
				return
			}
			a, err := p.agents.SetEnabled(id, action == "enable", body.Version)
			if err != nil {
				p.agentError(w, err)
				return
			}
			writeJSON(w, 200, a)
			return
		}
		a := agent.DefaultAgent()
		if r.Method == http.MethodPut {
			a.Lifecycle.Version = 0
		}
		if !decodeBody(w, r, &a) {
			return
		}
		var result *agent.Agent
		var err error
		status := 200
		if r.Method == http.MethodPost {
			result, err = p.agents.Create(&a)
			status = 201
		} else {
			result, err = p.agents.Update(id, &a, a.Lifecycle.Version)
		}
		if err != nil {
			p.agentError(w, err)
			return
		}
		writeJSON(w, status, result)
	case http.MethodDelete:
		version, err := strconv.ParseInt(r.URL.Query().Get("version"), 10, 64)
		if err != nil || version < 1 {
			apiError(w, 400, "INVALID_VERSION", "positive version is required")
			return
		}
		if err := p.agents.Delete(id, version); err != nil {
			p.agentError(w, err)
			return
		}
		writeJSON(w, 204, nil)
	}
}
