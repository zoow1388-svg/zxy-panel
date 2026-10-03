// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"log"
	"net/http"
	"sort"
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
	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	if !r.validateAgentTokenLocked(w, req, body.ServerID) {
		return
	}
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
	if body.BBRStatus != nil {
		body.BBRStatus.CheckedAt = time.Now()
		s.BBRStatus = *body.BBRStatus
	}
	logActor, logAction, logDetail := "", "", ""
	if body.CompletedActionID != "" && s.BBRPendingAction != nil && s.BBRPendingAction.ID == body.CompletedActionID {
		action := s.BBRPendingAction.Action
		s.BBRPendingAction = nil
		logActor, logAction, logDetail = "agent:"+body.ServerID, "bbr."+action+".complete", body.CompletedActionResult
	}
	s.UpdatedAt = time.Now()
	if err := r.store.SaveServerLocked(body.ServerID, s, logActor, logAction, req.RemoteAddr, logDetail); err != nil {
		log.Printf("failed to persist agent heartbeat: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save agent heartbeat"})
		return
	}
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
	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	if !r.validateAgentTokenLocked(w, req, body.ServerID) {
		return
	}
	server := r.store.Data.Servers[body.ServerID]
	nodes := make([]model.Node, 0)
	for _, n := range r.store.Data.Nodes {
		if n.ServerID == body.ServerID && n.Enabled {
			nodes = append(nodes, n)
		}
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Port != nodes[j].Port {
			return nodes[i].Port < nodes[j].Port
		}
		return nodes[i].ID < nodes[j].ID
	})
	relays := make([]model.RelayRoute, 0)
	for _, rr := range r.store.Data.RelayRoutes {
		if rr.RelayServerID == body.ServerID && rr.Enabled {
			relays = append(relays, rr)
		}
	}
	sort.SliceStable(relays, func(i, j int) bool {
		if relays[i].RelayPort != relays[j].RelayPort {
			return relays[i].RelayPort < relays[j].RelayPort
		}
		return relays[i].ID < relays[j].ID
	})
	clients := make([]model.Client, 0)
	for _, c := range r.store.Data.Clients {
		if c.Enabled {
			clients = append(clients, c)
		}
	}
	sort.SliceStable(clients, func(i, j int) bool {
		if clients[i].Username != clients[j].Username {
			return clients[i].Username < clients[j].Username
		}
		return clients[i].ID < clients[j].ID
	})
	cfg := xray.GenerateServerConfig(nodes, clients, relays, r.store.Data.Nodes, r.store.Data.NetworkPolicy)
	desiredHash := xray.ConfigHash(cfg)
	server.AgentVersion = body.AgentVersion
	server.LastSyncAt = time.Now()
	server.LastSyncMessage = "agent sync requested"
	server.UpdatedAt = time.Now()
	if err := r.store.SaveServerLocked(body.ServerID, server, "", "", "", ""); err != nil {
		log.Printf("failed to persist agent sync: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save agent sync"})
		return
	}
	writeJSON(w, http.StatusOK, model.AgentSyncResponse{
		OK:                  true,
		ServerID:            body.ServerID,
		DesiredConfigHash:   desiredHash,
		RestartRequired:     body.ConfigHash != desiredHash,
		XrayConfig:          cfg,
		NextIntervalSeconds: 30,
		Message:             "config generated",
		SystemAction:        server.BBRPendingAction,
	})
}

// Caller must hold the store lock through authentication and any mutation.
func (r *Router) validateAgentTokenLocked(w http.ResponseWriter, req *http.Request, serverID string) bool {
	if serverID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing server_id"})
		return false
	}
	token := req.Header.Get("X-Agent-Token")
	if token == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing agent token"})
		return false
	}
	s, ok := r.store.Data.Servers[serverID]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "server not registered"})
		return false
	}
	if token != s.AgentToken {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid agent token"})
		return false
	}
	if s.ID != serverID {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "server identity is inconsistent"})
		return false
	}
	return true
}
