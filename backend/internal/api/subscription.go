// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"zxy-panel/backend/internal/model"
)

func (r *Router) subscription(w http.ResponseWriter, req *http.Request) {
	path := strings.Trim(strings.TrimPrefix(req.URL.Path, "/sub/"), "/")
	parts := strings.Split(path, "/")
	token := ""
	if len(parts) > 0 {
		token = strings.TrimSpace(parts[0])
	}
	if token == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	format := strings.ToLower(strings.TrimSpace(req.URL.Query().Get("format")))
	if format == "" {
		format = strings.ToLower(strings.TrimSpace(req.URL.Query().Get("target")))
	}
	if format == "" && len(parts) > 1 {
		format = strings.ToLower(strings.TrimSpace(parts[1]))
	}
	isClash := format == "clash" || format == "clash-meta" || format == "mihomo" || format == "yaml" || format == "yml"

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
	nodes := []model.Node{}
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
		nodes = append(nodes, n)
	}
	relays := []model.RelayRoute{}
	for _, rid := range client.RelayRouteIDs {
		if rr, ok := r.store.Data.RelayRoutes[rid]; ok && rr.Enabled && rr.RouteMode == "socks5_route" {
			relays = append(relays, rr)
		}
	}
	if isClash {
		yaml := buildClashMetaSubscription(nodes, relays, client, r.store.Data.NetworkPolicy)
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if req.URL.Query().Get("download") == "1" || (len(parts) > 2 && strings.ToLower(parts[2]) == "download") {
			w.Header().Set("Content-Disposition", "attachment; filename=\"zxy-clash.yaml\"")
		}
		_, _ = w.Write([]byte(yaml))
		return
	}
	lines := []string{}
	for _, n := range nodes {
		lines = append(lines, buildVlessShareLink(n, client))
	}
	for _, rr := range relays {
		lines = append(lines, buildRelayShareLink(rr, client))
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(strings.Join(lines, "\n")))
}
func (r *Router) shortShare(w http.ResponseWriter, req *http.Request) {
	// V0.7.5.9.1: /s/<token>/<nodeID> 保留为兼容短链接接口。
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

func yamlQuote(v any) string {
	b, err := json.Marshal(fmt.Sprint(v))
	if err != nil {
		return "\"\""
	}
	return string(b)
}

func nodeHost(n model.Node) string {
	return valueOr(n.Host, "127.0.0.1")
}

func nodeShareName(n model.Node) string {
	if strings.TrimSpace(n.Name) != "" {
		return strings.TrimSpace(n.Name)
	}
	return fmt.Sprintf("%s:%d", nodeHost(n), n.Port)
}

func relayShareName(r model.RelayRoute) string {
	if strings.TrimSpace(r.Name) != "" {
		return strings.TrimSpace(r.Name)
	}
	return fmt.Sprintf("%s:%d", valueOr(r.RelayHost, "127.0.0.1"), r.RelayPort)
}

func buildClashProxyLines(name, host string, port int, uuid, security, transport, sni, fingerprint, publicKey, shortID, spiderX, path string) []string {
	security = strings.ToLower(valueOr(security, "none"))
	transport = strings.ToLower(valueOr(transport, "tcp"))
	lines := []string{}
	lines = append(lines, fmt.Sprintf("  - name: %s", yamlQuote(name)))
	lines = append(lines, "    type: vless")
	lines = append(lines, fmt.Sprintf("    server: %s", yamlQuote(host)))
	lines = append(lines, fmt.Sprintf("    port: %d", port))
	lines = append(lines, fmt.Sprintf("    uuid: %s", yamlQuote(uuid)))
	lines = append(lines, fmt.Sprintf("    network: %s", yamlQuote(transport)))
	lines = append(lines, "    udp: true")
	if security == "reality" || security == "tls" {
		lines = append(lines, "    tls: true")
		if strings.TrimSpace(sni) != "" {
			lines = append(lines, fmt.Sprintf("    servername: %s", yamlQuote(sni)))
		}
		lines = append(lines, fmt.Sprintf("    client-fingerprint: %s", yamlQuote(valueOr(fingerprint, "chrome"))))
		lines = append(lines, "    skip-cert-verify: false")
	} else {
		lines = append(lines, "    tls: false")
	}
	if security == "reality" {
		lines = append(lines, "    reality-opts:")
		lines = append(lines, fmt.Sprintf("      public-key: %s", yamlQuote(publicKey)))
		if strings.TrimSpace(shortID) != "" {
			lines = append(lines, fmt.Sprintf("      short-id: %s", yamlQuote(shortID)))
		}
	}
	if transport == "ws" && strings.TrimSpace(path) != "" {
		lines = append(lines, "    ws-opts:")
		lines = append(lines, fmt.Sprintf("      path: %s", yamlQuote(path)))
	}
	if transport == "grpc" && strings.TrimSpace(path) != "" {
		lines = append(lines, "    grpc-opts:")
		lines = append(lines, fmt.Sprintf("      grpc-service-name: %s", yamlQuote(strings.Trim(path, "/"))))
	}
	return lines
}

func buildClashMetaSubscription(nodes []model.Node, relays []model.RelayRoute, client model.Client, policy model.NetworkPolicy) string {
	proxyLines := []string{}
	proxyNames := []string{}
	for _, n := range nodes {
		name := nodeShareName(n)
		proxyNames = append(proxyNames, name)
		proxyLines = append(proxyLines, buildClashProxyLines(name, nodeHost(n), n.Port, client.UUID, n.Security, valueOr(n.Transport, "tcp"), n.SNI, n.Fingerprint, n.RealityPublicKey, n.RealityShortID, n.RealitySpiderX, n.Path)...)
	}
	for _, rr := range relays {
		name := relayShareName(rr)
		proxyNames = append(proxyNames, name)
		proxyLines = append(proxyLines, buildClashProxyLines(name, valueOr(rr.RelayHost, "127.0.0.1"), rr.RelayPort, client.UUID, "reality", "tcp", valueOr(rr.RelaySNI, "www.intel.com"), valueOr(rr.RelayFingerprint, "chrome"), rr.RelayRealityPublicKey, rr.RelayRealityShortID, rr.RelayRealitySpiderX, "")...)
	}
	nameservers := []string{"    - 1.1.1.1", "    - 8.8.8.8"}
	if policy.ClashIncludeQuad9 {
		nameservers = append(nameservers, "    - 9.9.9.9")
	}
	groupProxies := []string{}
	for _, name := range proxyNames {
		groupProxies = append(groupProxies, fmt.Sprintf("      - %s", yamlQuote(name)))
	}
	groupProxies = append(groupProxies, "      - DIRECT")
	if len(proxyLines) == 0 {
		proxyLines = []string{"  []"}
	}
	lines := []string{
		"# ZXY Panel Clash Verge / Mihomo profile",
		"# 仅支持 Clash Meta / Mihomo / Clash Verge Rev，旧版 Clash 不支持 VLESS Reality。",
		"mixed-port: 7890",
		"allow-lan: false",
		"mode: rule",
		"log-level: info",
		"ipv6: false",
		"global-client-fingerprint: chrome",
		"unified-delay: true",
		"tcp-concurrent: true",
		"dns:",
		"  enable: true",
		"  ipv6: false",
		"  enhanced-mode: fake-ip",
		"  nameserver:",
	}
	lines = append(lines, nameservers...)
	lines = append(lines, "proxies:")
	lines = append(lines, proxyLines...)
	lines = append(lines, "proxy-groups:")
	lines = append(lines, "  - name: PROXY")
	lines = append(lines, "    type: select")
	lines = append(lines, "    proxies:")
	lines = append(lines, groupProxies...)
	lines = append(lines, "rules:")
	lines = append(lines, "  - MATCH,PROXY")
	lines = append(lines, "")
	return strings.Join(lines, "\n")
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
		// V0.7.5.9.1: do not force flow in share URI.
		// The current Xray server-side client config uses an empty flow.
		// Adding flow=xtls-rprx-vision in the client URI while the server has no flow causes v2rayN to import successfully but fail to connect.
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
	// V0.7.5.9.1: relay share links must also match the server-side client flow.
	// Current generated Xray clients use empty flow, so the default QR/link must not include flow.
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
