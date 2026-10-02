// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"net/http"
	"strings"
	"time"

	"zxy-panel/backend/internal/model"
	"zxy-panel/backend/internal/store"
)

func (r *Router) servers(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.store.Mu.RLock()
		defer r.store.Mu.RUnlock()
		list := make([]model.Server, 0, len(r.store.Data.Servers))
		for _, item := range r.store.Data.Servers {
			list = append(list, item)
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var body model.Server
		if err := readJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		now := time.Now()
		body.ID = store.NewID("srv")
		if body.Status == "" {
			body.Status = "offline"
		}
		if body.AgentToken == "" {
			body.AgentToken = store.NewToken()
		}
		body.CreatedAt = now
		body.UpdatedAt = now
		r.store.Mu.Lock()
		defer r.store.Mu.Unlock()
		r.store.Data.Servers[body.ID] = body
		r.store.AddLog(currentClaims(req).Username, "server.create", clientIP(req), body.Name)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusCreated, body)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) serverByID(w http.ResponseWriter, req *http.Request) {
	id := strings.TrimPrefix(req.URL.Path, "/api/servers/")
	if id == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	item, ok := r.store.Data.Servers[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	switch req.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		var body model.Server
		if err := readJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		body.ID = id
		body.CreatedAt = item.CreatedAt
		body.UpdatedAt = time.Now()
		if body.AgentToken == "" {
			body.AgentToken = item.AgentToken
		}
		r.store.Data.Servers[id] = body
		r.store.AddLog(currentClaims(req).Username, "server.update", clientIP(req), id)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusOK, body)
	case http.MethodDelete:
		if conflicts := r.serverDeleteConflictsLocked(id); len(conflicts) > 0 {
			messages := make([]string, 0, len(conflicts))
			for i, conflict := range conflicts {
				if i == 0 || conflict.Kind != conflicts[i-1].Kind {
					messages = append(messages, conflict.Message)
				}
			}
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":     "Server deletion blocked: " + strings.Join(messages, " "),
				"code":      "server_delete_conflict",
				"conflicts": conflicts,
			})
			return
		}
		delete(r.store.Data.Servers, id)
		r.store.AddLog(currentClaims(req).Username, "server.delete", clientIP(req), id)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}
