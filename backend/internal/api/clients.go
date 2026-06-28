// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"zxy-panel/backend/internal/model"
	"zxy-panel/backend/internal/store"
	"zxy-panel/backend/internal/xray"
)

func (r *Router) clients(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.store.Mu.RLock()
		defer r.store.Mu.RUnlock()
		list := make([]model.Client, 0, len(r.store.Data.Clients))
		for _, item := range r.store.Data.Clients {
			list = append(list, item)
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var body model.Client
		if err := readJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		now := time.Now()
		body.ID = store.NewID("cli")
		if body.UUID == "" {
			body.UUID = store.NewUUID()
		}
		if body.SubscribeToken == "" {
			body.SubscribeToken = store.NewToken()
		}
		// 到期时间为空表示长期可用。
		body.Enabled = true
		body.CreatedAt = now
		body.UpdatedAt = now
		r.store.Mu.Lock()
		defer r.store.Mu.Unlock()
		if err := r.validateClientLocked(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		r.store.Data.Clients[body.ID] = body
		r.store.AddLog(currentClaims(req).Username, "client.create", clientIP(req), body.Username)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusCreated, body)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) clientByID(w http.ResponseWriter, req *http.Request) {
	rest := strings.TrimPrefix(req.URL.Path, "/api/clients/")
	if strings.HasSuffix(rest, "/reset-token") {
		id := strings.TrimSuffix(rest, "/reset-token")
		if req.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		r.store.Mu.Lock()
		defer r.store.Mu.Unlock()
		item, ok := r.store.Data.Clients[id]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		item.SubscribeToken = store.NewToken()
		item.UpdatedAt = time.Now()
		r.store.Data.Clients[id] = item
		r.store.AddLog(currentClaims(req).Username, "client.reset_token", clientIP(req), id)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusOK, item)
		return
	}
	if rest == "create-socks5-relay" {
		if req.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		r.createClientWithSocks5Relay(w, req)
		return
	}
	id := rest
	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	item, ok := r.store.Data.Clients[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	switch req.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		var body model.Client
		if err := readJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		body.ID = id
		body.CreatedAt = item.CreatedAt
		if body.UUID == "" {
			body.UUID = item.UUID
		}
		if body.SubscribeToken == "" {
			body.SubscribeToken = item.SubscribeToken
		}
		body.UpdatedAt = time.Now()
		if err := r.validateClientLocked(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		r.store.Data.Clients[id] = body
		r.store.AddLog(currentClaims(req).Username, "client.update", clientIP(req), id)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusOK, body)
	case http.MethodDelete:
		delete(r.store.Data.Clients, id)
		r.store.AddLog(currentClaims(req).Username, "client.delete", clientIP(req), id)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) validateClientLocked(c *model.Client) error {
	c.Username = strings.TrimSpace(c.Username)
	c.Email = strings.TrimSpace(c.Email)
	if c.Username == "" {
		return fmt.Errorf("请填写客户名")
	}
	clean := make([]string, 0, len(c.NodeIDs))
	seen := map[string]bool{}
	for _, id := range c.NodeIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		if _, ok := r.store.Data.Nodes[id]; !ok {
			return fmt.Errorf("绑定的节点不存在：%s", id)
		}
		seen[id] = true
		clean = append(clean, id)
	}
	c.NodeIDs = clean
	relayClean := make([]string, 0, len(c.RelayRouteIDs))
	seenRelays := map[string]bool{}
	for _, id := range c.RelayRouteIDs {
		id = strings.TrimSpace(id)
		if id == "" || seenRelays[id] {
			continue
		}
		rr, ok := r.store.Data.RelayRoutes[id]
		if !ok {
			return fmt.Errorf("绑定的中转线路不存在：%s", id)
		}
		if rr.RouteMode != "socks5_route" {
			return fmt.Errorf("客户固定出口只能绑定 SOCKS5 路由中转线路：%s", rr.Name)
		}
		seenRelays[id] = true
		relayClean = append(relayClean, id)
	}
	if len(relayClean) > 1 {
		return fmt.Errorf("固定出口客户只能绑定一条中转线路")
	}
	c.RelayRouteIDs = relayClean
	if c.TrafficLimitGB < 0 {
		return fmt.Errorf("流量限制不能小于 0")
	}
	return nil
}

type createClientRelayRequest struct {
	Username       string `json:"username"`
	Email          string `json:"email"`
	TrafficLimitGB int64  `json:"traffic_limit_gb"`
	ExpireAt       string `json:"expire_at"`
	RelayServerID  string `json:"relay_server_id"`
	RelayHost      string `json:"relay_host"`
	RelayPort      int    `json:"relay_port"`
	LandingExitID  string `json:"landing_exit_id"`
	RouteName      string `json:"route_name"`
	RelaySNI       string `json:"relay_sni"`
	RelayDest      string `json:"relay_reality_dest"`
	Fingerprint    string `json:"relay_fingerprint"`
	Remark         string `json:"remark"`
}

type createClientRelayResponse struct {
	Client model.Client      `json:"client"`
	Relay  model.RelayRoute  `json:"relay"`
	Exit   model.LandingExit `json:"exit"`
}

func (r *Router) createClientWithSocks5Relay(w http.ResponseWriter, req *http.Request) {
	var body createClientRelayRequest
	if err := readJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	now := time.Now()
	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	body.Username = strings.TrimSpace(body.Username)
	body.Email = strings.TrimSpace(body.Email)
	body.RelayServerID = strings.TrimSpace(body.RelayServerID)
	body.RelayHost = strings.TrimSpace(body.RelayHost)
	body.LandingExitID = strings.TrimSpace(body.LandingExitID)
	if body.Username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请填写客户名称"})
		return
	}
	if body.LandingExitID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请选择落地出口"})
		return
	}
	exit, ok := r.store.Data.LandingExits[body.LandingExitID]
	if !ok || !exit.Enabled {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "落地出口不存在或未启用"})
		return
	}
	if body.RelayServerID == "" {
		_ = r.store.EnsureSingleModeLocalServerLocked()
		body.RelayServerID = r.defaultServerIDLocked()
	}
	srv, ok := r.store.Data.Servers[body.RelayServerID]
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "中转服务器不存在"})
		return
	}
	if body.RelayHost == "" {
		if srv.Host != "" {
			body.RelayHost = srv.Host
		} else {
			body.RelayHost = srv.IP
		}
	}
	if body.RelayPort < 10000 || body.RelayPort > 60000 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "中转端口建议使用 10000-60000"})
		return
	}
	for _, n := range r.store.Data.Nodes {
		if n.ServerID == body.RelayServerID && n.Enabled && n.Port == body.RelayPort {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("中转端口 %d 已被入站 %s 使用", body.RelayPort, n.Name)})
			return
		}
	}
	for _, rr := range r.store.Data.RelayRoutes {
		if rr.RelayServerID == body.RelayServerID && rr.Enabled && rr.RelayPort == body.RelayPort {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("中转端口 %d 已被中转线路 %s 使用", body.RelayPort, rr.Name)})
			return
		}
	}
	priv, pub, err := xray.NewRealityKeys()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Reality 密钥生成失败：" + err.Error()})
		return
	}
	client := model.Client{
		ID: store.NewID("cli"), Username: body.Username, Email: body.Email, UUID: store.NewUUID(), SubscribeToken: store.NewToken(), TrafficLimitGB: body.TrafficLimitGB, Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	if body.ExpireAt != "" {
		if t, err := time.Parse(time.RFC3339, body.ExpireAt); err == nil {
			client.ExpireAt = t
		}
	}
	// 到期时间为空表示长期可用。
	if client.TrafficLimitGB == 0 {
		client.TrafficLimitGB = 100
	}
	name := strings.TrimSpace(body.RouteName)
	if name == "" {
		name = fmt.Sprintf("%s-%s固定出口", body.Username, exit.Host)
	}
	dest := strings.TrimSpace(body.RelayDest)
	if dest == "" {
		dest = "www.intel.com:443"
	}
	sni := strings.TrimSpace(body.RelaySNI)
	if sni == "" {
		sni = strings.Split(dest, ":")[0]
	}
	fp := strings.TrimSpace(body.Fingerprint)
	if fp == "" {
		fp = "chrome"
	}
	relay := model.RelayRoute{
		ID: store.NewID("relay"), Name: name, RelayServerID: body.RelayServerID, RelayHost: body.RelayHost, RelayPort: body.RelayPort,
		LandingMode: "manual_socks5", RouteMode: routeModeSocks5Route, RelayNetwork: "tcp",
		ManualSocksHost: exit.Host, ManualSocksPort: exit.Port, ManualSocksUsername: exit.Username, ManualSocksPassword: exit.Password, ManualSocksUDP: exit.UDP,
		RelaySNI: sni, RelayFingerprint: fp, RelayRealityDest: dest, RelayRealityPrivateKey: priv, RelayRealityPublicKey: pub, RelayRealityShortID: xray.NewRealityShortID(), RelayRealitySpiderX: "/",
		Remark: strings.TrimSpace(body.Remark), Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	client.RelayRouteIDs = []string{relay.ID}
	r.store.Data.RelayRoutes[relay.ID] = relay
	r.store.Data.Clients[client.ID] = client
	r.store.AddLog(currentClaims(req).Username, "client.create_relay", clientIP(req), fmt.Sprintf("%s -> %s:%d", client.Username, exit.Host, exit.Port))
	_ = r.store.SaveLocked()
	writeJSON(w, http.StatusCreated, createClientRelayResponse{Client: client, Relay: relay, Exit: exit})
}
