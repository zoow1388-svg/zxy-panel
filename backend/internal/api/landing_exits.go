// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"zxy-panel/backend/internal/model"
	"zxy-panel/backend/internal/store"
)

type landingExitBulkRequest struct {
	Text string `json:"text"`
}

func (r *Router) landingExits(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.store.Mu.RLock()
		defer r.store.Mu.RUnlock()
		list := make([]model.LandingExit, 0, len(r.store.Data.LandingExits))
		for _, item := range r.store.Data.LandingExits {
			list = append(list, item)
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPost:
		var body model.LandingExit
		if err := readJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		r.store.Mu.Lock()
		defer r.store.Mu.Unlock()
		if err := normalizeLandingExit(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		now := time.Now()
		body.ID = store.NewID("exit")
		body.Enabled = true
		body.CreatedAt = now
		body.UpdatedAt = now
		r.store.Data.LandingExits[body.ID] = body
		r.store.AddLog(currentClaims(req).Username, "exit.create", clientIP(req), body.Name)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusCreated, body)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) landingExitByID(w http.ResponseWriter, req *http.Request) {
	rest := strings.TrimPrefix(req.URL.Path, "/api/landing-exits/")
	if rest == "bulk" {
		r.bulkLandingExits(w, req)
		return
	}
	if strings.HasSuffix(rest, "/test") {
		id := strings.TrimSuffix(rest, "/test")
		r.testLandingExit(w, req, id)
		return
	}
	id := rest
	if id == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	item, ok := r.store.Data.LandingExits[id]
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	switch req.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, item)
	case http.MethodPut:
		var body model.LandingExit
		if err := readJSON(req, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
		if err := normalizeLandingExit(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		body.ID = id
		body.CreatedAt = item.CreatedAt
		body.LastTestIP = item.LastTestIP
		body.LastTestMsg = item.LastTestMsg
		body.LastTestAt = item.LastTestAt
		body.UpdatedAt = time.Now()
		r.store.Data.LandingExits[id] = body
		r.store.AddLog(currentClaims(req).Username, "exit.update", clientIP(req), id)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusOK, body)
	case http.MethodDelete:
		for _, rr := range r.store.Data.RelayRoutes {
			if rr.RouteMode == routeModeSocks5Route && rr.ManualSocksHost == item.Host && rr.ManualSocksPort == item.Port && rr.ManualSocksUsername == item.Username {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "该出口已有中转线路使用，请先删除或迁移对应线路"})
				return
			}
		}
		delete(r.store.Data.LandingExits, id)
		r.store.AddLog(currentClaims(req).Username, "exit.delete", clientIP(req), id)
		_ = r.store.SaveLocked()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		methodNotAllowed(w)
	}
}

func normalizeLandingExit(e *model.LandingExit) error {
	e.Name = strings.TrimSpace(e.Name)
	e.Host = strings.TrimSpace(e.Host)
	e.Username = strings.TrimSpace(e.Username)
	e.Password = strings.TrimSpace(e.Password)
	e.Region = strings.TrimSpace(e.Region)
	e.Provider = strings.TrimSpace(e.Provider)
	e.Remark = strings.TrimSpace(e.Remark)
	if e.Host == "" {
		return fmt.Errorf("请填写落地出口 IP 或域名")
	}
	if e.Port < 1 || e.Port > 65535 {
		return fmt.Errorf("落地 SOCKS5 端口必须在 1-65535 之间")
	}
	if e.Username == "" || e.Password == "" {
		return fmt.Errorf("落地 SOCKS5 必须填写账号和密码，避免出口裸奔")
	}
	if e.Name == "" {
		e.Name = fmt.Sprintf("%s:%d", e.Host, e.Port)
	}
	if e.BandwidthMbps < 0 {
		e.BandwidthMbps = 0
	}
	return nil
}

func (r *Router) bulkLandingExits(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body landingExitBulkRequest
	if err := readJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	lines := strings.Split(body.Text, "\n")
	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	created := 0
	errs := []string{}
	for idx, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := splitBulkLine(line)
		if len(parts) < 5 {
			errs = append(errs, fmt.Sprintf("第%d行格式错误，应为：名称,IP,端口,账号,密码[,地区,备注]", idx+1))
			continue
		}
		port, err := strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil {
			errs = append(errs, fmt.Sprintf("第%d行端口错误", idx+1))
			continue
		}
		item := model.LandingExit{Name: parts[0], Host: parts[1], Port: port, Username: parts[3], Password: parts[4]}
		if len(parts) > 5 {
			item.Region = parts[5]
		}
		if len(parts) > 6 {
			item.Remark = parts[6]
		}
		if err := normalizeLandingExit(&item); err != nil {
			errs = append(errs, fmt.Sprintf("第%d行：%s", idx+1, err.Error()))
			continue
		}
		now := time.Now()
		item.ID = store.NewID("exit")
		item.Enabled = true
		item.CreatedAt = now
		item.UpdatedAt = now
		r.store.Data.LandingExits[item.ID] = item
		created++
	}
	if created > 0 {
		r.store.AddLog(currentClaims(req).Username, "exit.bulk", clientIP(req), fmt.Sprintf("created=%d", created))
		_ = r.store.SaveLocked()
	}
	writeJSON(w, http.StatusOK, map[string]any{"created": created, "errors": errs})
}

func splitBulkLine(line string) []string {
	if strings.Contains(line, "|") {
		return cleanParts(strings.Split(line, "|"))
	}
	return cleanParts(strings.Split(line, ","))
}

func cleanParts(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func (r *Router) testLandingExit(w http.ResponseWriter, req *http.Request, id string) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	r.store.Mu.RLock()
	item, ok := r.store.Data.LandingExits[id]
	r.store.Mu.RUnlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	started := time.Now()
	ip, err := socks5HTTPGet(item.Host, item.Port, item.Username, item.Password, "api.ipify.org", 80, "api.ipify.org", "/")
	if err != nil || net.ParseIP(strings.TrimSpace(ip)) == nil {
		ip, err = socks5HTTPGet(item.Host, item.Port, item.Username, item.Password, "ifconfig.me", 80, "ifconfig.me", "/ip")
	}
	cost := time.Since(started).Milliseconds()
	item.LastTestAt = time.Now()
	if err != nil || net.ParseIP(strings.TrimSpace(ip)) == nil {
		msg := "出口检测失败"
		if err != nil {
			msg = err.Error()
		} else {
			msg = "SOCKS5 已连接，但出口 IP 返回异常"
		}
		item.LastTestMsg = msg
		r.store.Mu.Lock()
		r.store.Data.LandingExits[id] = item
		_ = r.store.SaveLocked()
		r.store.Mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "latency_ms": cost, "message": msg})
		return
	}
	item.LastTestIP = strings.TrimSpace(ip)
	item.LastTestMsg = "出口检测成功"
	r.store.Mu.Lock()
	r.store.Data.LandingExits[id] = item
	_ = r.store.SaveLocked()
	r.store.Mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "exit_ip": item.LastTestIP, "latency_ms": cost, "message": item.LastTestMsg})
}
