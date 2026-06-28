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

func (r *Router) nodes(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.store.Mu.Lock()
		defer r.store.Mu.Unlock()
		_ = r.store.EnsureSingleModeLocalServerLocked()
		list := make([]model.Node, 0, len(r.store.Data.Nodes))
		for _, item := range r.store.Data.Nodes {
			list = append(list, item)
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var body model.Node
		if err := readJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		r.store.Mu.Lock()
		defer r.store.Mu.Unlock()
		if err := r.normalizeNodeLocked(&body, ""); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		now := time.Now()
		body.ID = store.NewID("node")
		body.Enabled = true
		body.CreatedAt = now
		body.UpdatedAt = now
		r.store.Data.Nodes[body.ID] = body
		r.store.AddLog(currentClaims(req).Username, "node.create", clientIP(req), body.Name)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusCreated, body)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) nodeByID(w http.ResponseWriter, req *http.Request) {
	rest := strings.TrimPrefix(req.URL.Path, "/api/nodes/")
	if rest == "reality-keys" {
		if req.Method != http.MethodPost && req.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		priv, pub, err := xray.NewRealityKeys()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"private_key": priv, "public_key": pub, "short_id": xray.NewRealityShortID(), "spider_x": "/", "fingerprint": "chrome", "dest": "www.intel.com:443", "sni": "www.intel.com"})
		return
	}
	if strings.HasSuffix(rest, "/xray-config") {
		id := strings.TrimSuffix(rest, "/xray-config")
		r.store.Mu.RLock()
		node, ok := r.store.Data.Nodes[id]
		clients := make([]model.Client, 0)
		for _, c := range r.store.Data.Clients {
			if c.Enabled {
				clients = append(clients, c)
			}
		}
		r.store.Mu.RUnlock()
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		cfg := xray.GenerateInboundConfig(node, clients)
		writeJSON(w, http.StatusOK, cfg)
		return
	}
	id := rest
	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	item, ok := r.store.Data.Nodes[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	switch req.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		var body model.Node
		if err := readJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		if err := r.normalizeNodeLocked(&body, id); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		body.ID = id
		body.CreatedAt = item.CreatedAt
		body.UpdatedAt = time.Now()
		r.store.Data.Nodes[id] = body
		r.store.AddLog(currentClaims(req).Username, "node.update", clientIP(req), id)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusOK, body)
	case http.MethodDelete:
		delete(r.store.Data.Nodes, id)
		// 从客户绑定列表中同步移除，避免订阅里残留不存在的节点。
		for cid, c := range r.store.Data.Clients {
			next := make([]string, 0, len(c.NodeIDs))
			for _, nid := range c.NodeIDs {
				if nid != id {
					next = append(next, nid)
				}
			}
			c.NodeIDs = next
			r.store.Data.Clients[cid] = c
		}
		r.store.AddLog(currentClaims(req).Username, "node.delete", clientIP(req), id)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) defaultServerIDLocked() string {
	var best model.Server
	found := false
	for _, s := range r.store.Data.Servers {
		if !found {
			best = s
			found = true
			continue
		}
		if s.Status == "online" && best.Status != "online" {
			best = s
			continue
		}
		if s.LastSyncAt.After(best.LastSyncAt) {
			best = s
		}
	}
	if !found {
		return ""
	}
	return best.ID
}

func (r *Router) normalizeNodeLocked(n *model.Node, currentID string) error {
	n.ServerID = strings.TrimSpace(n.ServerID)
	n.Name = strings.TrimSpace(n.Name)
	n.Protocol = strings.ToLower(strings.TrimSpace(n.Protocol))
	n.Host = strings.TrimSpace(n.Host)
	n.Transport = strings.ToLower(strings.TrimSpace(n.Transport))
	n.Security = strings.ToLower(strings.TrimSpace(n.Security))
	n.SNI = strings.TrimSpace(n.SNI)
	n.Path = strings.TrimSpace(n.Path)
	n.Fingerprint = strings.TrimSpace(n.Fingerprint)
	n.RealityDest = strings.TrimSpace(n.RealityDest)
	n.RealityPrivateKey = strings.TrimSpace(n.RealityPrivateKey)
	n.RealityPublicKey = strings.TrimSpace(n.RealityPublicKey)
	n.RealityShortID = strings.TrimSpace(n.RealityShortID)
	n.RealitySpiderX = strings.TrimSpace(n.RealitySpiderX)
	n.SocksUsername = strings.TrimSpace(n.SocksUsername)
	n.SocksPassword = strings.TrimSpace(n.SocksPassword)
	n.Remark = strings.TrimSpace(n.Remark)

	if n.ServerID == "" {
		_ = r.store.EnsureSingleModeLocalServerLocked()
		n.ServerID = r.defaultServerIDLocked()
	}
	if n.ServerID == "" {
		return fmt.Errorf("单机服务器尚未初始化，请重新运行安装脚本或到高级服务器管理检查本机服务器")
	}
	srv, ok := r.store.Data.Servers[n.ServerID]
	if !ok {
		return fmt.Errorf("服务器不存在，请到高级服务器管理检查本机服务器")
	}
	if n.Name == "" {
		return fmt.Errorf("请填写节点名称")
	}
	if n.Protocol == "" {
		n.Protocol = "vless"
	}
	if n.Transport == "" {
		n.Transport = "tcp"
	}
	if n.Security == "" {
		n.Security = "none"
	}
	if n.Host == "" {
		if srv.Host != "" {
			n.Host = srv.Host
		} else {
			n.Host = srv.IP
		}
	}
	if n.SNI == "" && n.Security != "none" {
		n.SNI = n.Host
	}
	if n.Path == "" && n.Transport == "ws" {
		n.Path = "/zxy"
	}
	if n.Protocol == "socks" || n.Protocol == "socks5" {
		n.Protocol = "socks"
		n.Transport = "tcp"
		n.Security = "none"
		n.Path = ""
		n.SNI = ""
		n.Fingerprint = ""
		n.RealityDest = ""
		n.RealityPrivateKey = ""
		n.RealityPublicKey = ""
		n.RealityShortID = ""
		n.RealitySpiderX = ""
		if n.SocksUsername == "" {
			n.SocksUsername = "zxy"
		}
		if n.SocksPassword == "" {
			n.SocksPassword = store.NewToken()
		}
	}
	if n.Security == "reality" {
		// V0.6.1.1 推荐模式：Reality 先固定为 tcp，减少新手把 ws/grpc 和 Reality 混用导致配置异常。
		n.Transport = "tcp"
		n.Path = ""
		if n.RealityDest == "" {
			n.RealityDest = "www.intel.com:443"
		}
		if n.SNI == "" {
			n.SNI = strings.Split(n.RealityDest, ":")[0]
		}
		if n.Fingerprint == "" {
			n.Fingerprint = "chrome"
		}
		if n.RealitySpiderX == "" {
			n.RealitySpiderX = "/"
		}
		if n.RealityShortID == "" {
			n.RealityShortID = xray.NewRealityShortID()
		}
		if n.RealityPrivateKey == "" || n.RealityPublicKey == "" {
			if n.RealityPrivateKey != "" && n.RealityPublicKey == "" {
				pub, err := xray.PublicKeyFromPrivate(n.RealityPrivateKey)
				if err != nil {
					return fmt.Errorf("Reality 私钥格式不正确，请点击重新生成")
				}
				n.RealityPublicKey = pub
			} else {
				priv, pub, err := xray.NewRealityKeys()
				if err != nil {
					return fmt.Errorf("Reality 密钥生成失败：%v", err)
				}
				n.RealityPrivateKey = priv
				n.RealityPublicKey = pub
			}
		}
	}
	if n.Port < 1 || n.Port > 65535 {
		return fmt.Errorf("端口必须在 1-65535 之间")
	}
	if n.Enabled {
		for id, existing := range r.store.Data.Nodes {
			if id == currentID {
				continue
			}
			if existing.ServerID == n.ServerID && existing.Enabled && existing.Port == n.Port {
				return fmt.Errorf("端口 %d 已被节点 %s 使用，请换一个端口或先停用原节点", n.Port, existing.Name)
			}
		}
	}
	switch n.Protocol {
	case "vless", "vmess", "trojan", "shadowsocks", "socks":
	default:
		return fmt.Errorf("暂不支持的协议：%s", n.Protocol)
	}
	if n.Protocol == "socks" {
		if n.SocksUsername == "" || n.SocksPassword == "" {
			return fmt.Errorf("SOCKS5 入站必须设置账号和密码，避免落地出口裸奔")
		}
		return nil
	}
	switch n.Transport {
	case "tcp", "ws", "grpc":
	default:
		return fmt.Errorf("暂不支持的传输方式：%s", n.Transport)
	}
	switch n.Security {
	case "none", "tls", "reality":
	default:
		return fmt.Errorf("暂不支持的安全方式：%s", n.Security)
	}
	return nil
}
