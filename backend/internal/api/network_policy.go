// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"zxy-panel/backend/internal/model"
)

type networkPolicyPayload struct {
	Policy model.NetworkPolicy `json:"policy"`
}

func (r *Router) networkPolicy(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case http.MethodGet:
		r.networkPolicyStatus(w, req)
	case http.MethodPut, http.MethodPost:
		r.networkPolicySave(w, req)
	default:
		methodNotAllowed(w)
	}
}

func (r *Router) networkPolicyPreview(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	var body networkPolicyPayload
	if err := readJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	p := normalizePolicyForAPI(body.Policy)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"policy":           p,
		"summary":          policySummary(p),
		"warnings":         policyWarnings(p),
		"xray_dns_preview": xrayDNSPreview(p),
		"routing_preview":  routingPreview(p),
		"message":          "这是预览，不会修改当前配置。点击应用策略后才会保存，并等待 Agent 下一次同步重启网络核心。",
	})
}

func (r *Router) networkPolicyRollback(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	backup := normalizePolicyForAPI(r.store.Data.NetworkPolicyBackup)
	current := normalizePolicyForAPI(r.store.Data.NetworkPolicy)
	r.store.Data.NetworkPolicyBackup = current
	backup.UpdatedAt = time.Now()
	backup.UpdatedBy = currentClaims(req).Username
	r.store.Data.NetworkPolicy = backup
	r.store.AddLog(currentClaims(req).Username, "network_policy_rollback", req.RemoteAddr, "回滚到上一次网络策略："+backup.Mode)
	if err := r.store.SaveLocked(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"policy":  backup,
		"message": "已回滚到上一次网络策略。Agent 下一次同步后会重新生成并应用 Xray 配置。",
	})
}

func (r *Router) networkPolicyStatus(w http.ResponseWriter, req *http.Request) {
	r.store.Mu.RLock()
	defer r.store.Mu.RUnlock()
	p := normalizePolicyForAPI(r.store.Data.NetworkPolicy)
	backup := normalizePolicyForAPI(r.store.Data.NetworkPolicyBackup)
	writeJSON(w, http.StatusOK, map[string]any{
		"version":          panelVersion,
		"policy":           p,
		"backup":           backup,
		"summary":          policySummary(p),
		"warnings":         policyWarnings(p),
		"xray_dns_preview": xrayDNSPreview(p),
		"routing_preview":  routingPreview(p),
		"modes": []map[string]string{
			{"value": "compat", "label": "兼容稳定模式", "desc": "默认推荐：保留稳定 DNS 与 UseIPv4，不阻断 53，不禁用 fallback，不阻断 QUIC。"},
			{"value": "public_dns", "label": "公共 DNS 稳定模式", "desc": "使用 1.1.1.1 / 8.8.8.8 / 9.9.9.9 与 UseIPv4，不启用强阻断。"},
			{"value": "dns_leak_guard", "label": "DNS 防泄漏增强模式", "desc": "偏向减少 DNS 漂移，仍不强制阻断主链路，适合 AI / Google / 海外社媒场景。"},
			{"value": "strict", "label": "严格防泄漏模式", "desc": "手动启用；可能导致变慢或兼容性下降，适合已确认 DNS 泄漏的高级用户。"},
			{"value": "custom", "label": "自定义模式", "desc": "完全由用户自己调整 DNS、fallback、QUIC、IPv6 和阻断选项。"},
		},
		"message": "网络策略中心只负责配置能力。升级不会自动启用强阻断，应用策略前会备份当前策略。",
	})
}

func (r *Router) networkPolicySave(w http.ResponseWriter, req *http.Request) {
	var body networkPolicyPayload
	if err := readJSON(req, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	p := normalizePolicyForAPI(body.Policy)
	warnings := policyWarnings(p)
	if p.Mode == "strict" && !strings.Contains(strings.ToLower(req.URL.Query().Get("confirm")), "yes") {
		// 前端会二次确认；这里不强制阻断保存，只给出显式提示。
		warnings = append(warnings, "严格模式可能导致部分网站变慢、DNS 解析失败或软路由兼容性下降。")
	}

	r.store.Mu.Lock()
	defer r.store.Mu.Unlock()
	old := normalizePolicyForAPI(r.store.Data.NetworkPolicy)
	r.store.Data.NetworkPolicyBackup = old
	p.UpdatedAt = time.Now()
	p.UpdatedBy = currentClaims(req).Username
	r.store.Data.NetworkPolicy = p
	r.store.AddLog(currentClaims(req).Username, "network_policy_apply", req.RemoteAddr, fmt.Sprintf("应用网络策略：%s -> %s", old.Mode, p.Mode))
	if err := r.store.SaveLocked(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"policy":   p,
		"backup":   old,
		"summary":  policySummary(p),
		"warnings": warnings,
		"message":  "网络策略已保存。Agent 下一次同步会测试并应用新 Xray 配置；如出现异常，可点击回滚到上一次网络策略。",
	})
}

func normalizePolicyForAPI(p model.NetworkPolicy) model.NetworkPolicy {
	mode := strings.TrimSpace(p.Mode)
	if mode == "" {
		mode = "compat"
	}
	allowedModes := map[string]bool{"compat": true, "public_dns": true, "dns_leak_guard": true, "strict": true, "custom": true}
	if !allowedModes[mode] {
		mode = "compat"
	}
	p.Mode = mode

	switch mode {
	case "compat":
		p.PublicDNS = true
		p.DNSServers = []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}
		p.QueryStrategy = "UseIPv4"
		p.DisableFallback = false
		p.DisableFallbackIfMatch = false
		p.BlockDNS53 = false
		p.BlockChinaDNS = false
		p.BlockQUIC = false
		if p.IPv6Strategy == "" {
			p.IPv6Strategy = "keep"
		}
		p.ClashIncludeQuad9 = false
		p.SingBoxIncludeQuad9 = false
	case "public_dns":
		p.PublicDNS = true
		p.DNSServers = cleanDNSServers(p.DNSServers, true)
		p.QueryStrategy = "UseIPv4"
		p.DisableFallback = false
		p.DisableFallbackIfMatch = false
		p.BlockDNS53 = false
		p.BlockChinaDNS = false
		p.BlockQUIC = false
		if p.IPv6Strategy == "" {
			p.IPv6Strategy = "keep"
		}
		p.ClashIncludeQuad9 = true
		p.SingBoxIncludeQuad9 = true
	case "dns_leak_guard":
		p.PublicDNS = true
		p.DNSServers = cleanDNSServers(p.DNSServers, true)
		p.QueryStrategy = "UseIPv4"
		p.DisableFallback = false
		p.DisableFallbackIfMatch = false
		p.BlockDNS53 = false
		p.BlockChinaDNS = false
		p.BlockQUIC = false
		if p.IPv6Strategy == "" || p.IPv6Strategy == "keep" {
			p.IPv6Strategy = "warn"
		}
		p.ClashIncludeQuad9 = true
		p.SingBoxIncludeQuad9 = true
	case "strict":
		p.PublicDNS = true
		p.DNSServers = cleanDNSServers(p.DNSServers, true)
		p.QueryStrategy = "UseIPv4"
		p.DisableFallback = true
		p.DisableFallbackIfMatch = true
		p.BlockDNS53 = true
		p.BlockChinaDNS = true
		p.BlockQUIC = true
		if p.IPv6Strategy == "" || p.IPv6Strategy == "keep" {
			p.IPv6Strategy = "disable_hint"
		}
		p.ClashIncludeQuad9 = true
		p.SingBoxIncludeQuad9 = true
	case "custom":
		p.DNSServers = cleanDNSServers(p.DNSServers, p.PublicDNS)
		if p.QueryStrategy == "" {
			p.QueryStrategy = "AsIs"
		}
		if p.IPv6Strategy == "" {
			p.IPv6Strategy = "keep"
		}
	}
	allowedQuery := map[string]bool{"AsIs": true, "UseIPv4": true, "UseIPv6": true, "UseIP": true}
	if !allowedQuery[p.QueryStrategy] {
		p.QueryStrategy = "AsIs"
	}
	allowedIPv6 := map[string]bool{"keep": true, "warn": true, "disable_hint": true}
	if !allowedIPv6[p.IPv6Strategy] {
		p.IPv6Strategy = "keep"
	}
	return p
}

func cleanDNSServers(list []string, useDefault bool) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range list {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		host := v
		if strings.Contains(v, "://") {
			// Xray 允许 https+local:// 等格式，这里只做最小清洗，不做强拦截。
			host = strings.TrimPrefix(strings.TrimPrefix(v, "https+local://"), "tcp+local://")
		}
		ip := net.ParseIP(host)
		if ip == nil && !strings.Contains(v, "://") {
			continue
		}
		if !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	if len(out) == 0 && useDefault {
		out = []string{"1.1.1.1", "8.8.8.8", "9.9.9.9"}
	}
	return out
}

func policySummary(p model.NetworkPolicy) []string {
	items := []string{}
	modeName := map[string]string{"compat": "兼容稳定模式", "public_dns": "公共 DNS 稳定模式", "dns_leak_guard": "DNS 防泄漏增强模式", "strict": "严格防泄漏模式", "custom": "自定义模式"}[p.Mode]
	if modeName == "" {
		modeName = p.Mode
	}
	items = append(items, "当前模式："+modeName)
	if p.PublicDNS || len(p.DNSServers) > 0 {
		items = append(items, "DNS 服务器："+strings.Join(p.DNSServers, " / "))
	} else {
		items = append(items, "DNS 服务器：跟随默认配置")
	}
	items = append(items, "查询策略："+p.QueryStrategy)
	items = append(items, yesNo("禁用 fallback", p.DisableFallback))
	items = append(items, yesNo("阻断 53 端口", p.BlockDNS53))
	items = append(items, yesNo("阻断中国公共 DNS", p.BlockChinaDNS))
	items = append(items, yesNo("阻断 QUIC UDP 443", p.BlockQUIC))
	items = append(items, "IPv6 策略："+ipv6StrategyLabel(p.IPv6Strategy))
	return items
}

func policyWarnings(p model.NetworkPolicy) []string {
	warnings := []string{}
	if p.Mode == "strict" || p.DisableFallback || p.DisableFallbackIfMatch || p.BlockDNS53 || p.BlockQUIC {
		warnings = append(warnings, "当前策略包含严格选项，可能造成网页首次打开变慢、部分域名解析失败、软路由兼容性下降。")
	}
	if p.BlockDNS53 {
		warnings = append(warnings, "阻断 tcp/udp 53 只建议在确认 DNS 泄漏后启用，不建议作为默认生产策略。")
	}
	if p.BlockQUIC {
		warnings = append(warnings, "阻断 QUIC 可能影响 Google / YouTube / Gemini 首次连接体验，但可减少部分 UDP 旁路风险。")
	}
	if p.IPv6Strategy == "disable_hint" {
		warnings = append(warnings, "IPv6 禁用建议需要在客户端或软路由侧执行，面板不会自动修改客户本地网络。")
	}
	return warnings
}

func xrayDNSPreview(p model.NetworkPolicy) map[string]any {
	dns := map[string]any{}
	if p.PublicDNS || len(p.DNSServers) > 0 {
		dns["servers"] = p.DNSServers
	}
	if p.QueryStrategy != "" && p.QueryStrategy != "AsIs" {
		dns["queryStrategy"] = p.QueryStrategy
	}
	if p.DisableFallback {
		dns["disableFallback"] = true
	}
	if p.DisableFallbackIfMatch {
		dns["disableFallbackIfMatch"] = true
	}
	return dns
}

func routingPreview(p model.NetworkPolicy) []string {
	out := []string{}
	if p.BlockDNS53 {
		out = append(out, "将增加 tcp/udp 53 阻断规则")
	}
	if p.BlockChinaDNS {
		out = append(out, "将增加常见中国公共 DNS IP 阻断规则")
	}
	if p.BlockQUIC {
		out = append(out, "将增加 UDP 443 / QUIC 阻断规则")
	}
	if len(out) == 0 {
		out = append(out, "不增加强制阻断规则")
	}
	return out
}

func yesNo(label string, ok bool) string {
	if ok {
		return label + "：已启用"
	}
	return label + "：未启用"
}

func ipv6StrategyLabel(v string) string {
	switch v {
	case "warn":
		return "检测提醒"
	case "disable_hint":
		return "建议禁用"
	default:
		return "不处理"
	}
}
