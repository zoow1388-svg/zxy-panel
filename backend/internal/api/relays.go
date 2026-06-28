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

const (
	routeModeTCPForward  = "tcp_forward"
	routeModeSocks5Route = "socks5_route"
)

func (r *Router) relays(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.store.Mu.RLock()
		defer r.store.Mu.RUnlock()
		list := make([]model.RelayRoute, 0, len(r.store.Data.RelayRoutes))
		for _, item := range r.store.Data.RelayRoutes {
			list = append(list, item)
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var body model.RelayRoute
		if err := readJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		r.store.Mu.Lock()
		defer r.store.Mu.Unlock()
		if err := r.normalizeRelayLocked(&body, ""); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		now := time.Now()
		body.ID = store.NewID("relay")
		body.Enabled = true
		body.CreatedAt = now
		body.UpdatedAt = now
		r.store.Data.RelayRoutes[body.ID] = body
		r.store.AddLog(currentClaims(req).Username, "relay.create", clientIP(req), body.Name)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusCreated, body)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) relayByID(w http.ResponseWriter, req *http.Request) {
	id := strings.TrimPrefix(req.URL.Path, "/api/relays/")
	if id == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	item, ok := r.store.Data.RelayRoutes[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	switch req.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		var body model.RelayRoute
		if err := readJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		if err := r.normalizeRelayLocked(&body, id); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		body.ID = id
		body.CreatedAt = item.CreatedAt
		body.UpdatedAt = time.Now()
		r.store.Data.RelayRoutes[id] = body
		r.store.AddLog(currentClaims(req).Username, "relay.update", clientIP(req), id)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusOK, body)
	case http.MethodDelete:
		delete(r.store.Data.RelayRoutes, id)
		for cid, c := range r.store.Data.Clients {
			next := make([]string, 0, len(c.RelayRouteIDs))
			for _, rid := range c.RelayRouteIDs {
				if rid != id {
					next = append(next, rid)
				}
			}
			c.RelayRouteIDs = next
			r.store.Data.Clients[cid] = c
		}
		r.store.AddLog(currentClaims(req).Username, "relay.delete", clientIP(req), id)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) normalizeRelayLocked(relay *model.RelayRoute, currentID string) error {
	relay.Name = strings.TrimSpace(relay.Name)
	relay.RelayServerID = strings.TrimSpace(relay.RelayServerID)
	relay.LandingNodeID = strings.TrimSpace(relay.LandingNodeID)
	relay.RelayHost = strings.TrimSpace(relay.RelayHost)
	relay.LandingMode = strings.ToLower(strings.TrimSpace(relay.LandingMode))
	relay.ManualSocksHost = strings.TrimSpace(relay.ManualSocksHost)
	relay.ManualSocksUsername = strings.TrimSpace(relay.ManualSocksUsername)
	relay.ManualSocksPassword = strings.TrimSpace(relay.ManualSocksPassword)
	relay.RouteMode = strings.ToLower(strings.TrimSpace(relay.RouteMode))
	relay.RelayNetwork = strings.ToLower(strings.TrimSpace(relay.RelayNetwork))
	relay.RelaySNI = strings.TrimSpace(relay.RelaySNI)
	relay.RelayFingerprint = strings.TrimSpace(relay.RelayFingerprint)
	relay.RelayRealityDest = strings.TrimSpace(relay.RelayRealityDest)
	relay.RelayRealityPrivateKey = strings.TrimSpace(relay.RelayRealityPrivateKey)
	relay.RelayRealityPublicKey = strings.TrimSpace(relay.RelayRealityPublicKey)
	relay.RelayRealityShortID = strings.TrimSpace(relay.RelayRealityShortID)
	relay.RelayRealitySpiderX = strings.TrimSpace(relay.RelayRealitySpiderX)
	relay.Remark = strings.TrimSpace(relay.Remark)

	if relay.Name == "" {
		return fmt.Errorf("请填写中转名称，例如：美国中转01")
	}
	if relay.RouteMode == "" {
		relay.RouteMode = routeModeTCPForward
	}
	switch relay.RouteMode {
	case routeModeTCPForward, routeModeSocks5Route:
	default:
		return fmt.Errorf("中转类型只支持 TCP 透传中转或 SOCKS5 路由中转")
	}
	if relay.RelayServerID == "" {
		_ = r.store.EnsureSingleModeLocalServerLocked()
		relay.RelayServerID = r.defaultServerIDLocked()
	}
	srv, ok := r.store.Data.Servers[relay.RelayServerID]
	if !ok {
		return fmt.Errorf("中转服务器不存在，请先到服务器管理添加或检查 Agent")
	}
	if relay.RelayHost == "" {
		if srv.Host != "" {
			relay.RelayHost = srv.Host
		} else {
			relay.RelayHost = srv.IP
		}
	}
	if relay.RelayPort < 1 || relay.RelayPort > 65535 {
		return fmt.Errorf("中转端口必须在 1-65535 之间")
	}
	if relay.RelayPort < 10000 {
		return fmt.Errorf("中转端口建议使用 10000-60000，高端口更适合当前服务器环境")
	}

	switch relay.RouteMode {
	case routeModeTCPForward:
		if relay.LandingNodeID == "" {
			return fmt.Errorf("TCP 透传中转请选择 VLESS Reality 落地节点")
		}
		landing, ok := r.store.Data.Nodes[relay.LandingNodeID]
		if !ok || !landing.Enabled {
			return fmt.Errorf("落地节点不存在或未启用")
		}
		if landing.Protocol != "vless" || landing.Security != "reality" {
			return fmt.Errorf("TCP 透传中转仅支持 VLESS + Reality 落地节点；SOCKS5 落地请改用 SOCKS5 路由中转")
		}
		if relay.RelayNetwork == "" {
			relay.RelayNetwork = "tcp"
		}
		switch relay.RelayNetwork {
		case "tcp", "udp", "tcp,udp":
		default:
			return fmt.Errorf("中转协议只支持 tcp、udp 或 tcp,udp")
		}
		// VLESS Reality 落地节点本身是 TCP 协议，UDP-only 中转会导致 V2rayN / 小火箭无法建立连接。
		// 在 Hysteria2 / TUIC 等原生 UDP 落地协议加入前，禁止把 UDP-only 绑定到 VLESS Reality TCP 落地。
		if landing.Protocol == "vless" && landing.Security == "reality" && (landing.Transport == "" || landing.Transport == "tcp") && relay.RelayNetwork == "udp" {
			return fmt.Errorf("当前落地节点为 VLESS Reality TCP，不支持 UDP-only 中转；请改用 TCP。TCP+UDP 仅作为实验模式，不建议正式使用")
		}
	case routeModeSocks5Route:
		if relay.LandingMode == "" {
			if relay.LandingNodeID != "" {
				relay.LandingMode = "panel_node"
			} else {
				relay.LandingMode = "manual_socks5"
			}
		}
		switch relay.LandingMode {
		case "panel_node":
			if relay.LandingNodeID == "" {
				return fmt.Errorf("请选择本面板 SOCKS5 落地入站，或切换为手动填写远程 SOCKS5")
			}
			landing, ok := r.store.Data.Nodes[relay.LandingNodeID]
			if !ok || !landing.Enabled {
				return fmt.Errorf("落地 SOCKS5 入站不存在或未启用")
			}
			if landing.Protocol != "socks" {
				return fmt.Errorf("SOCKS5 路由中转必须选择 SOCKS5 落地入站")
			}
			if strings.TrimSpace(landing.SocksUsername) == "" || strings.TrimSpace(landing.SocksPassword) == "" {
				return fmt.Errorf("落地 SOCKS5 必须设置账号和密码，避免出口裸奔")
			}
		case "manual_socks5":
			relay.LandingNodeID = ""
			if relay.ManualSocksHost == "" {
				return fmt.Errorf("请填写远程落地 SOCKS5 地址，例如 203.0.113.10")
			}
			if relay.ManualSocksPort < 1 || relay.ManualSocksPort > 65535 {
				return fmt.Errorf("远程落地 SOCKS5 端口必须在 1-65535 之间")
			}
			if relay.ManualSocksUsername == "" || relay.ManualSocksPassword == "" {
				return fmt.Errorf("远程落地 SOCKS5 必须填写账号和密码")
			}
		default:
			return fmt.Errorf("SOCKS5 落地方式只支持本面板入站或手动填写远程 SOCKS5")
		}
		relay.RelayNetwork = "tcp"
		if relay.RelayRealityDest == "" {
			relay.RelayRealityDest = "www.intel.com:443"
		}
		if relay.RelaySNI == "" {
			relay.RelaySNI = strings.Split(relay.RelayRealityDest, ":")[0]
		}
		if relay.RelayFingerprint == "" {
			relay.RelayFingerprint = "chrome"
		}
		if relay.RelayRealitySpiderX == "" {
			relay.RelayRealitySpiderX = "/"
		}
		if relay.RelayRealityShortID == "" {
			relay.RelayRealityShortID = xray.NewRealityShortID()
		}
		if relay.RelayRealityPrivateKey == "" || relay.RelayRealityPublicKey == "" {
			if relay.RelayRealityPrivateKey != "" && relay.RelayRealityPublicKey == "" {
				pub, err := xray.PublicKeyFromPrivate(relay.RelayRealityPrivateKey)
				if err != nil {
					return fmt.Errorf("中转 Reality 私钥格式不正确")
				}
				relay.RelayRealityPublicKey = pub
			} else {
				priv, pub, err := xray.NewRealityKeys()
				if err != nil {
					return fmt.Errorf("中转 Reality 密钥生成失败：%v", err)
				}
				relay.RelayRealityPrivateKey = priv
				relay.RelayRealityPublicKey = pub
			}
		}
	}

	for id, n := range r.store.Data.Nodes {
		if n.ServerID == relay.RelayServerID && n.Enabled && n.Port == relay.RelayPort {
			return fmt.Errorf("中转端口 %d 已被入站 %s 使用，请换一个端口", relay.RelayPort, n.Name)
		}
		_ = id
	}
	for id, rr := range r.store.Data.RelayRoutes {
		if id == currentID {
			continue
		}
		if rr.RelayServerID == relay.RelayServerID && rr.Enabled && rr.RelayPort == relay.RelayPort {
			return fmt.Errorf("中转端口 %d 已被中转线路 %s 使用，请换一个端口", relay.RelayPort, rr.Name)
		}
	}
	return nil
}
