// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
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
		if err := r.store.SaveServerLocked(body.ID, body, currentClaims(req).Username, "server.create", clientIP(req), body.Name); err != nil {
			log.Printf("failed to persist server creation: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save server creation; no changes committed."})
			return
		}
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
	if req.Method == http.MethodGet {
		r.store.Mu.RLock()
		defer r.store.Mu.RUnlock()
		item, exists := r.store.Data.Servers[id]
		if !exists {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, item)
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
	case http.MethodPut:
		if item.ID != id {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "Server identity is inconsistent; no changes committed."})
			return
		}
		var fields map[string]json.RawMessage
		if err := readJSON(req, &fields); err != nil || fields == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		body, status, err := mergeServerUpdate(item, fields)
		if err != nil {
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		body.UpdatedAt = time.Now()
		if err := r.store.SaveServerLocked(id, body, currentClaims(req).Username, "server.update", clientIP(req), id); err != nil {
			log.Printf("failed to persist server update: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save server update; no changes committed."})
			return
		}
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
		if err := r.store.DeleteServerLocked(id, currentClaims(req).Username, clientIP(req)); err != nil {
			log.Printf("failed to persist server deletion: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "Failed to save server deletion; the server and its references have not been changed.",
				"code":  "server_delete_save_failed",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func mergeServerUpdate(current model.Server, fields map[string]json.RawMessage) (model.Server, int, error) {
	normalized := make(map[string]json.RawMessage, len(fields))
	for name, raw := range fields {
		name = strings.ToLower(name)
		if _, exists := normalized[name]; exists {
			return current, http.StatusBadRequest, fmt.Errorf("ambiguous server field: %s", name)
		}
		normalized[name] = raw
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return current, http.StatusBadRequest, fmt.Errorf("invalid server json")
	}
	var provided model.Server
	if err := json.Unmarshal(raw, &provided); err != nil {
		return current, http.StatusBadRequest, fmt.Errorf("invalid server field type")
	}
	next := current
	editable := []struct {
		name        string
		value       string
		destination *string
	}{
		{"name", provided.Name, &next.Name}, {"ip", provided.IP, &next.IP},
		{"host", provided.Host, &next.Host}, {"region", provided.Region, &next.Region},
		{"provider", provided.Provider, &next.Provider},
	}
	for _, field := range editable {
		if raw, present := normalized[field.name]; present {
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return current, http.StatusBadRequest, fmt.Errorf("server field %s cannot be null", field.name)
			}
			*field.destination = field.value
		}
	}
	protected := []struct {
		name              string
		current, provided any
	}{
		{"id", current.ID, provided.ID}, {"created_at", current.CreatedAt, provided.CreatedAt},
		{"agent_token", current.AgentToken, provided.AgentToken}, {"status", current.Status, provided.Status},
		{"agent_version", current.AgentVersion, provided.AgentVersion}, {"xray_version", current.XrayVersion, provided.XrayVersion},
		{"config_hash", current.ConfigHash, provided.ConfigHash}, {"last_sync_at", current.LastSyncAt, provided.LastSyncAt},
		{"last_sync_message", current.LastSyncMessage, provided.LastSyncMessage},
		{"cpu_usage", current.CPUUsage, provided.CPUUsage}, {"memory_usage", current.MemoryUsage, provided.MemoryUsage},
		{"disk_usage", current.DiskUsage, provided.DiskUsage}, {"upload_total", current.UploadTotal, provided.UploadTotal},
		{"download_total", current.DownloadTotal, provided.DownloadTotal},
		{"bbr_status", current.BBRStatus, provided.BBRStatus}, {"bbr_pending_action", current.BBRPendingAction, provided.BBRPendingAction},
	}
	for _, field := range protected {
		raw, present := normalized[field.name]
		if !present {
			continue
		}
		if field.name != "bbr_pending_action" && bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return current, http.StatusBadRequest, fmt.Errorf("server field %s cannot be null", field.name)
		}
		before, err := json.Marshal(field.current)
		if err != nil {
			return current, http.StatusInternalServerError, fmt.Errorf("stored server field cannot be serialized")
		}
		after, err := json.Marshal(field.provided)
		if err != nil {
			return current, http.StatusBadRequest, fmt.Errorf("invalid server field")
		}
		if !bytes.Equal(before, after) {
			return current, http.StatusConflict, fmt.Errorf("server field %s is protected; refresh the server before editing metadata", field.name)
		}
	}
	return next, http.StatusOK, nil
}
