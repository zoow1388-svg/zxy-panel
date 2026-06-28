// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"net/http"
	"sort"
	"time"
)

func (r *Router) dashboard(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	r.store.Mu.RLock()
	defer r.store.Mu.RUnlock()

	var enabledServers, enabledNodes, enabledClients int
	var upload, download int64
	var serverInfo map[string]any

	for _, s := range r.store.Data.Servers {
		if s.Status == "online" {
			enabledServers++
		}
		upload += s.UploadTotal
		download += s.DownloadTotal
		if serverInfo == nil || s.Status == "online" {
			serverInfo = map[string]any{
				"id":            s.ID,
				"name":          s.Name,
				"ip":            s.IP,
				"host":          s.Host,
				"status":        s.Status,
				"agent_version": s.AgentVersion,
				"xray_version":  s.XrayVersion,
				"last_sync_at":  s.LastSyncAt,
				"last_message":  s.LastSyncMessage,
				"cpu_usage":     s.CPUUsage,
				"memory_usage":  s.MemoryUsage,
				"disk_usage":    s.DiskUsage,
			}
		}
	}

	for _, n := range r.store.Data.Nodes {
		if n.Enabled {
			enabledNodes++
		}
	}
	for _, c := range r.store.Data.Clients {
		if c.Enabled {
			enabledClients++
		}
	}

	recentNodes := make([]map[string]any, 0, len(r.store.Data.Nodes))
	for _, n := range r.store.Data.Nodes {
		recentNodes = append(recentNodes, map[string]any{
			"id":         n.ID,
			"name":       n.Name,
			"host":       n.Host,
			"port":       n.Port,
			"protocol":   n.Protocol,
			"transport":  n.Transport,
			"security":   n.Security,
			"enabled":    n.Enabled,
			"updated_at": n.UpdatedAt,
			"created_at": n.CreatedAt,
		})
	}
	sort.Slice(recentNodes, func(i, j int) bool {
		return dashboardTime(recentNodes[i]["updated_at"], recentNodes[i]["created_at"]).After(dashboardTime(recentNodes[j]["updated_at"], recentNodes[j]["created_at"]))
	})
	if len(recentNodes) > 5 {
		recentNodes = recentNodes[:5]
	}

	recentClients := make([]map[string]any, 0, len(r.store.Data.Clients))
	for _, c := range r.store.Data.Clients {
		recentClients = append(recentClients, map[string]any{
			"id":               c.ID,
			"username":         c.Username,
			"email":            c.Email,
			"enabled":          c.Enabled,
			"traffic_limit_gb": c.TrafficLimitGB,
			"traffic_used_gb":  c.TrafficUsedGB,
			"expire_at":        c.ExpireAt,
			"updated_at":       c.UpdatedAt,
			"created_at":       c.CreatedAt,
		})
	}
	sort.Slice(recentClients, func(i, j int) bool {
		return dashboardTime(recentClients[i]["updated_at"], recentClients[i]["created_at"]).After(dashboardTime(recentClients[j]["updated_at"], recentClients[j]["created_at"]))
	})
	if len(recentClients) > 5 {
		recentClients = recentClients[:5]
	}

	recentLogs := make([]map[string]any, 0, len(r.store.Data.OperationLogs))
	for _, l := range r.store.Data.OperationLogs {
		recentLogs = append(recentLogs, map[string]any{
			"id":         l.ID,
			"actor":      l.Actor,
			"action":     l.Action,
			"ip":         l.IP,
			"detail":     l.Detail,
			"created_at": l.CreatedAt,
		})
	}
	sort.Slice(recentLogs, func(i, j int) bool {
		return dashboardTime(recentLogs[i]["created_at"]).After(dashboardTime(recentLogs[j]["created_at"]))
	})
	if len(recentLogs) > 8 {
		recentLogs = recentLogs[:8]
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"version":         panelVersion,
		"servers":         len(r.store.Data.Servers),
		"online_servers":  enabledServers,
		"nodes":           len(r.store.Data.Nodes),
		"enabled_nodes":   enabledNodes,
		"clients":         len(r.store.Data.Clients),
		"enabled_clients": enabledClients,
		"upload_total":    upload,
		"download_total":  download,
		"primary_server":  serverInfo,
		"recent_nodes":    recentNodes,
		"recent_clients":  recentClients,
		"recent_logs":     recentLogs,
	})
}

func dashboardTime(values ...any) time.Time {
	for _, v := range values {
		if t, ok := v.(time.Time); ok && !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}
