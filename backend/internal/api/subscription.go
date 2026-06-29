// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"zxy-panel/backend/internal/model"
)

func (r *Router) subscription(w http.ResponseWriter, req *http.Request) {
	token := strings.TrimPrefix(req.URL.Path, "/sub/")
	if token == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	r.store.Mu.RLock()
	defer r.store.Mu.RUnlock()
	var client model.Client
	found := false
	for _, c := range r.store.Data.Clients {
		if c.SubscribeToken == token {
			client = c
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "subscription not found", http.StatusNotFound)
		return
	}
	if !client.Enabled || (!client.ExpireAt.IsZero() && client.ExpireAt.Before(time.Now())) {
		http.Error(w, "subscription expired or disabled", http.StatusForbidden)
		return
	}
	allowed := map[string]bool{}
	for _, id := range client.NodeIDs {
		allowed[id] = true
	}
	fixedExitOnly := len(client.RelayRouteIDs) > 0 && len(client.NodeIDs) == 0
	lines := []string{}
	for _, n := range r.store.Data.Nodes {
		if !n.Enabled {
			continue
		}
		if fixedExitOnly {
			continue
		}
		if len(allowed) > 0 && !allowed[n.ID] {
			continue
		}
		if strings.ToLower(n.Protocol) != "vless" {
			continue
		}
		lines = append(lines, buildVlessShareLink(n, client))
	}
	for _, rid := range client.RelayRouteIDs {
		if rr, ok := r.store.Data.RelayRoutes[rid]; ok && rr.Enabled && rr.RouteMode == "socks5_route" {
			lines = append(lines, buildRelayShareLink(rr, client))
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(strings.Join(lines, "\n")))
}
func (r *Router) shortShare(w http.ResponseWriter, req *http.Request) {
	// V0.7.5.9: /s/<token>/<nodeID> 保留为兼容短链接接口。
	// 默认二维码不再使用 HTTP 短链接，而是直接编码 vless:// 单节点链接。
	parts := strings.Split(strings.TrimPrefix(req.URL.Path, "/s/"), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "short share not found", http.StatusNotFound)
		return
	}
	token := parts[0]
	nodeID := parts[1]

	r.store.Mu.RLock()
	defer r.store.Mu.RUnlock()

	var client model.Client
	foundClient := false
	for _, c := range r.store.Data.Clients {
		if c.SubscribeToken == token {
			client = c
			foundClient = true
			break
		}
	}
	if !foundClient {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}
	if !client.Enabled || (!client.ExpireAt.IsZero() && client.ExpireAt.Before(time.Now())) {
		http.Error(w, "client expired or disabled", http.StatusForbidden)
		return
	}

	allowed := map[string]bool{}
	for _, id := range client.NodeIDs {
		allowed[id] = true
	}
	fixedExitOnly := len(client.RelayRouteIDs) > 0 && len(client.NodeIDs) == 0
	if fixedExitOnly {
		if _, ok := r.store.Data.RelayRoutes[nodeID]; !ok {
			http.Error(w, "fixed-exit client cannot use normal node", http.StatusForbidden)
			return
		}
	}
	if len(allowed) > 0 && !allowed[nodeID] {
		http.Error(w, "node not allowed", http.StatusForbidden)
		return
	}

	var node model.Node
	foundNode := false
	for _, n := range r.store.Data.Nodes {
		if n.ID == nodeID {
			node = n
			foundNode = true
			break
		}
	}
	if foundNode && node.Enabled && strings.ToLower(node.Protocol) == "vless" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(buildVlessShareLink(node, client)))
		return
	}
	if rr, ok := r.store.Data.RelayRoutes[nodeID]; ok && rr.Enabled && rr.RouteMode == "socks5_route" {
		allowedRelay := false
		for _, rid := range client.RelayRouteIDs {
			if rid == rr.ID {
				allowedRelay = true
				break
			}
		}
		if !allowedRelay && len(client.RelayRouteIDs) > 0 {
			http.Error(w, "relay not allowed", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(buildRelayShareLink(rr, client)))
		return
	}
	http.Error(w, "node not found", http.StatusNotFound)
}

func buildVlessShareLink(n model.Node, client model.Client) string {
	host := n.Host
	if host == "" {
		host = "127.0.0.1"
	}
	q := url.Values{}
	q.Set("encryption", "none")
	q.Set("security", valueOr(n.Security, "none"))
	if n.SNI != "" {
		q.Set("sni", n.SNI)
	}
	if strings.ToLower(n.Security) == "reality" {
		q.Set("flow", "xtls-rprx-vision")
		q.Set("fp", valueOr(n.Fingerprint, "chrome"))
		if n.RealityPublicKey != "" {
			q.Set("pbk", n.RealityPublicKey)
		}
		if n.RealityShortID != "" {
			q.Set("sid", n.RealityShortID)
		}
		q.Set("spx", valueOr(n.RealitySpiderX, "/"))
	}
	q.Set("type", valueOr(n.Transport, "tcp"))
	if strings.ToLower(n.Transport) == "ws" && n.Path != "" {
		q.Set("path", n.Path)
	}
	if strings.ToLower(n.Transport) == "grpc" && n.Path != "" {
		q.Set("serviceName", strings.Trim(n.Path, "/"))
	}
	name := url.QueryEscape(n.Name)
	return fmt.Sprintf("vless://%s@%s:%d?%s#%s", client.UUID, host, n.Port, q.Encode(), name)
}

func buildRelayShareLink(r model.RelayRoute, client model.Client) string {
	q := url.Values{}
	q.Set("encryption", "none")
	q.Set("flow", "xtls-rprx-vision")
	q.Set("security", "reality")
	q.Set("sni", valueOr(r.RelaySNI, "www.intel.com"))
	q.Set("fp", valueOr(r.RelayFingerprint, "chrome"))
	if r.RelayRealityPublicKey != "" {
		q.Set("pbk", r.RelayRealityPublicKey)
	}
	if r.RelayRealityShortID != "" {
		q.Set("sid", r.RelayRealityShortID)
	}
	q.Set("spx", valueOr(r.RelayRealitySpiderX, "/"))
	q.Set("type", "tcp")
	name := url.QueryEscape(r.Name)
	return fmt.Sprintf("vless://%s@%s:%d?%s#%s", client.UUID, valueOr(r.RelayHost, "127.0.0.1"), r.RelayPort, q.Encode(), name)
}

func valueOr(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
