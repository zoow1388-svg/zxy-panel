// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"net/http"
	"sort"
	"time"

	"zxy-panel/backend/internal/model"
	"zxy-panel/backend/internal/store"
)

const (
	bbrStatusAction  = "bbr-status"
	bbrEnableAction  = "enable-bbr"
	bbrDisableAction = "disable-bbr"
)

type bbrActionRequest struct {
	ServerID string `json:"server_id"`
}

type bbrServerStatus struct {
	ID            string                   `json:"id"`
	Name          string                   `json:"name"`
	Host          string                   `json:"host"`
	Status        string                   `json:"status"`
	AgentVersion  string                   `json:"agent_version"`
	BBR           model.BBRStatus          `json:"bbr"`
	PendingAction *model.AgentSystemAction `json:"pending_action,omitempty"`
}

func (r *Router) bbrStatus(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.store.Mu.Lock()
		defer r.store.Mu.Unlock()
		_ = r.store.EnsureSingleModeLocalServerLocked()

		items := make([]bbrServerStatus, 0, len(r.store.Data.Servers))
		for _, server := range r.store.Data.Servers {
			items = append(items, bbrServerStatus{
				ID:            server.ID,
				Name:          server.Name,
				Host:          server.Host,
				Status:        server.Status,
				AgentVersion:  server.AgentVersion,
				BBR:           server.BBRStatus,
				PendingAction: server.BBRPendingAction,
			})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
		writeJSON(w, http.StatusOK, map[string]any{"servers": items})
	case http.MethodPost:
		r.queueBBRAction(w, req, bbrStatusAction)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) bbrEnable(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	r.queueBBRAction(w, req, bbrEnableAction)
}

func (r *Router) bbrDisable(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	r.queueBBRAction(w, req, bbrDisableAction)
}

func (r *Router) queueBBRAction(w http.ResponseWriter, req *http.Request, action string) {
	var body bbrActionRequest
	if err := readJSON(req, &body); err != nil || body.ServerID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "server_id is required"})
		return
	}

	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	server, ok := r.store.Data.Servers[body.ServerID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "server not found"})
		return
	}

	pending := &model.AgentSystemAction{
		ID:          store.NewID("bbr"),
		Action:      action,
		RequestedAt: time.Now(),
		RequestedBy: currentClaims(req).Username,
	}
	server.BBRPendingAction = pending
	server.UpdatedAt = time.Now()
	r.store.Data.Servers[server.ID] = server
	r.store.AddLog(currentClaims(req).Username, "bbr."+action, clientIP(req), server.Name)
	if err := r.store.SaveLocked(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save BBR action"})
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"ok":             true,
		"server_id":      server.ID,
		"pending_action": pending,
		"message":        "BBR action queued for the host agent",
	})
}
