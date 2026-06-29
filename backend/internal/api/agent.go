// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"net/http"
	"time"

	"zxy-panel/backend/internal/model"
	"zxy-panel/backend/internal/xray"
)

func (r *Router) agentHeartbeat(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body model.AgentHeartbeat
	if err := readJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if !r.validateAgentToken(w, req, body.ServerID) {
		return
	}
	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	s := r.store.Data.Servers[body.ServerID]
	s.Status = "online"
	s.AgentVersion = body.AgentVersion
	s.XrayVersion = body.XrayVersion
	s.ConfigHash = body.ConfigHash
	s.LastSyncMessage = body.LastMessage
	s.CPUUsage = body.CPUUsage
	s.MemoryUsage = body.MemoryUsage
	s.DiskUsage = body.DiskUsage
	s.UploadTotal = body.UploadTotal
	s.DownloadTotal = body.DownloadTotal
	s.UpdatedAt = time.Now()
	r.store.Data.Servers[s.ID] = s
	_ = r.store.SaveLocked()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "next_interval_seconds": 30})
}

func (r *Router) agentSync(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body model.AgentSyncRequest
	if err := readJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if !r.validateAgentToken(w, req, body.ServerID) {
		return
	}
	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	server, ok := r.store.Data.Servers[body.ServerID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "server not registered"})
		return
	}
	nodes := make([]model.Node, 0)
	for _, n := range r.store.Data.Nodes {
		if n.ServerID == body.ServerID && n.Enabled {
			nodes = append(nodes, n)
		}
	}
	relays := make([]model.RelayRoute, 0)
	for _, rr := range r.store.Data.RelayRoutes {
		if rr.RelayServerID == body.ServerID && rr.Enabled {
			relays = append(relays, rr)
		}
	}
	clients := make([]model.Client, 0)
	for _, c := range r.store.Data.Clients {
		if c.Enabled {
			clients = append(clients, c)
		}
	}
	cfg := xray.GenerateServerConfig(nodes, clients, relays, r.store.Data.Nodes, r.store.Data.NetworkPolicy)
	desiredHash := xray.ConfigHash(cfg)
	server.AgentVersion = body.AgentVersion
	server.LastSyncAt = time.Now()
	server.LastSyncMessage = "agent sync requested"
	server.UpdatedAt = time.Now()
	r.store.Data.Servers[server.ID] = server
	_ = r.store.SaveLocked()
	writeJSON(w, http.StatusOK, model.AgentSyncResponse{
		OK:                  true,
		ServerID:            body.ServerID,
		DesiredConfigHash:   desiredHash,
		RestartRequired:     body.ConfigHash != desiredHash,
		XrayConfig:          cfg,
		NextIntervalSeconds: 30,
		Message:             "config generated",
	})
}

func (r *Router) validateAgentToken(w http.ResponseWriter, req *http.Request, serverID string) bool {
	if serverID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing server_id"})
		return false
	}
	token := req.Header.Get("X-Agent-Token")
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing agent token"})
		return false
	}
	r.store.Mu.RLock()
	s, ok := r.store.Data.Servers[serverID]
	r.store.Mu.RUnlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "server not registered"})
		return false
	}
	if token != s.AgentToken {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid agent token"})
		return false
	}
	return true
}
