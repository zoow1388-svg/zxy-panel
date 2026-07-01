// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"zxy-panel/backend/internal/model"
	"zxy-panel/backend/internal/security"
	"zxy-panel/backend/internal/xray"
)

const panelVersion = "0.7.6.2-clean-release-fix-agent-xray"
const installDir = "/opt/zxy-panel"

type systemCheck struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

func (r *Router) systemChecks(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	checks, counts := r.buildSystemChecks()
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     panelVersion,
		"install_dir": installDir,
		"checked_at":  time.Now().Format(time.RFC3339),
		"counts":      counts,
		"checks":      checks,
	})
}

func (r *Router) systemReport(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	checks, counts := r.buildSystemChecks()

	r.store.Mu.RLock()
	defer r.store.Mu.RUnlock()

	var b strings.Builder
	b.WriteString("ZXY Panel 诊断报告\n")
	b.WriteString("====================\n")
	b.WriteString("生成时间：" + time.Now().Format(time.RFC3339) + "\n")
	b.WriteString("面板版本：" + panelVersion + "\n")
	b.WriteString("安装目录：" + installDir + "\n")
	b.WriteString(fmt.Sprintf("服务器：%d，在线：%d，节点：%d，客户：%d，落地出口：%d，中转线路：%d，SOCKS5落地：%d，SOCKS5路由中转：%d\n\n", counts["servers"], counts["online_servers"], counts["nodes"], counts["clients"], counts["landing_exits"], counts["relays"], counts["socks_nodes"], counts["socks5_route_relays"]))

	b.WriteString("检测结果：\n")
	for _, c := range checks {
		b.WriteString(fmt.Sprintf("- [%s] %s：%s\n", strings.ToUpper(c.Status), c.Label, c.Message))
	}

	b.WriteString("\n服务器：\n")
	servers := make([]model.Server, 0, len(r.store.Data.Servers))
	for _, s := range r.store.Data.Servers {
		servers = append(servers, s)
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].CreatedAt.Before(servers[j].CreatedAt) })
	for _, s := range servers {
		b.WriteString(fmt.Sprintf("- %s | %s | %s | status=%s | agent=%s | xray=%s | sync=%s | msg=%s\n", s.Name, s.IP, s.Host, s.Status, emptyDash(s.AgentVersion), emptyDash(s.XrayVersion), timeOrDash(s.LastSyncAt), emptyDash(s.LastSyncMessage)))
	}

	b.WriteString("\n节点：\n")
	nodes := make([]model.Node, 0, len(r.store.Data.Nodes))
	for _, n := range r.store.Data.Nodes {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].CreatedAt.Before(nodes[j].CreatedAt) })
	if len(nodes) == 0 {
		b.WriteString("- 暂无节点。\n")
	}
	for _, n := range nodes {
		b.WriteString(fmt.Sprintf("- %s | server=%s | %s:%d | %s/%s/%s | enabled=%v\n", n.Name, n.ServerID, n.Host, n.Port, n.Protocol, n.Transport, n.Security, n.Enabled))
	}

	b.WriteString("\n落地出口：\n")
	exits := make([]model.LandingExit, 0, len(r.store.Data.LandingExits))
	for _, e := range r.store.Data.LandingExits {
		exits = append(exits, e)
	}
	sort.Slice(exits, func(i, j int) bool { return exits[i].CreatedAt.Before(exits[j].CreatedAt) })
	if len(exits) == 0 {
		b.WriteString("- 暂无落地出口。\n")
	}
	for _, e := range exits {
		b.WriteString(fmt.Sprintf("- %s | %s:%d | user=%s | region=%s | bandwidth=%dM | last_ip=%s | enabled=%v\n", e.Name, e.Host, e.Port, e.Username, emptyDash(e.Region), e.BandwidthMbps, emptyDash(e.LastTestIP), e.Enabled))
	}

	b.WriteString("\n中转线路：\n")
	relays := make([]model.RelayRoute, 0, len(r.store.Data.RelayRoutes))
	for _, rr := range r.store.Data.RelayRoutes {
		relays = append(relays, rr)
	}
	sort.Slice(relays, func(i, j int) bool { return relays[i].CreatedAt.Before(relays[j].CreatedAt) })
	if len(relays) == 0 {
		b.WriteString("- 暂无中转线路。\n")
	}
	for _, rr := range relays {
		relaySrv := r.store.Data.Servers[rr.RelayServerID]
		landingText := "-"
		if relayRouteMode(rr.RouteMode) == "socks5_route" && strings.ToLower(strings.TrimSpace(rr.LandingMode)) == "manual_socks5" {
			landingText = fmt.Sprintf("manual-socks5 %s:%d", rr.ManualSocksHost, rr.ManualSocksPort)
		} else {
			landing := r.store.Data.Nodes[rr.LandingNodeID]
			landingText = fmt.Sprintf("%s %s:%d", emptyDash(landing.Name), landing.Host, landing.Port)
		}
		b.WriteString(fmt.Sprintf("- %s | mode=%s | relay=%s %s:%d | network=%s | landing=%s | enabled=%v | remark=%s\n", rr.Name, relayRouteMode(rr.RouteMode), emptyDash(relaySrv.Name), rr.RelayHost, rr.RelayPort, relayNetworkOrTCP(rr.RelayNetwork), landingText, rr.Enabled, emptyDash(rr.Remark)))
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

func (r *Router) buildSystemChecks() ([]systemCheck, map[string]int) {
	r.store.Mu.RLock()
	defer r.store.Mu.RUnlock()

	checks := []systemCheck{}
	onlineServers := 0
	staleServers := 0
	versionMismatch := 0
	defaultPassword := false
	now := time.Now()
	for _, s := range r.store.Data.Servers {
		if s.Status == "online" {
			onlineServers++
		}
		if s.LastSyncAt.IsZero() || now.Sub(s.LastSyncAt) > 2*time.Minute {
			staleServers++
		}
		if s.AgentVersion != "" && s.AgentVersion != panelVersion {
			versionMismatch++
		}
	}
	for _, a := range r.store.Data.Admins {
		if a.Username == "admin" && security.VerifyPassword("admin123", a.PasswordHash) {
			defaultPassword = true
			break
		}
	}

	status := "ok"
	msg := "管理员密码已修改。"
	if defaultPassword {
		status = "warn"
		msg = "仍在使用默认密码 admin123，请马上到系统设置修改。"
	}
	checks = append(checks, systemCheck{Key: "admin_password", Label: "管理员密码", Status: status, Message: msg})

	if len(r.store.Data.Servers) == 0 {
		checks = append(checks, systemCheck{Key: "servers", Label: "服务器接入", Status: "warn", Message: "还没有服务器，单机模式会自动创建本机服务器；如果没有服务器，请重新运行安装脚本。"})
	} else if onlineServers == 0 {
		checks = append(checks, systemCheck{Key: "servers", Label: "服务器接入", Status: "warn", Message: "服务器已添加，但 Agent 暂未在线。"})
	} else {
		checks = append(checks, systemCheck{Key: "servers", Label: "服务器接入", Status: "ok", Message: "已有 Agent 在线。"})
	}

	if len(r.store.Data.Servers) > 0 && staleServers > 0 {
		checks = append(checks, systemCheck{Key: "sync", Label: "Agent 同步", Status: "warn", Message: "有服务器超过 2 分钟没有同步，建议检查 zxy-agent 状态。"})
	} else if len(r.store.Data.Servers) > 0 {
		checks = append(checks, systemCheck{Key: "sync", Label: "Agent 同步", Status: "ok", Message: "服务器同步状态正常。"})
	}

	if versionMismatch > 0 {
		checks = append(checks, systemCheck{Key: "agent_version", Label: "Agent 版本", Status: "warn", Message: "有服务器 Agent 版本与面板版本不一致，请重新复制一键安装命令执行。"})
	} else if onlineServers > 0 {
		checks = append(checks, systemCheck{Key: "agent_version", Label: "Agent 版本", Status: "ok", Message: "在线服务器 Agent 版本与面板一致。"})
	}

	checks = append(checks, systemCheck{Key: "install_command", Label: "安装命令", Status: "ok", Message: "当前安装目录为 " + installDir + "。复制失败时页面会显示完整命令，可手动复制。"})

	if len(r.store.Data.Servers) == 1 {
		checks = append(checks, systemCheck{Key: "single_mode", Label: "单机模式", Status: "ok", Message: "已初始化本机服务器，入站会默认绑定本机，无需手动选择服务器。"})
	} else if len(r.store.Data.Servers) > 1 {
		checks = append(checks, systemCheck{Key: "multi_server_mode", Label: "多服务器模式", Status: "ok", Message: "当前已启用主控 + 中转/远程服务器架构。多台服务器记录属于正常状态。"})
	}

	loopbackServer := false
	for _, s := range r.store.Data.Servers {
		if isLoopbackForReport(s.IP) || isLoopbackForReport(s.Host) {
			loopbackServer = true
		}
	}
	if loopbackServer {
		checks = append(checks, systemCheck{Key: "public_endpoint", Label: "公网入口", Status: "warn", Message: "本机服务器仍显示 127.0.0.1/localhost，建议重新运行 V0.7.5 安装脚本修正公网 IP。"})
	} else if len(r.store.Data.Servers) > 0 {
		checks = append(checks, systemCheck{Key: "public_endpoint", Label: "公网入口", Status: "ok", Message: "本机服务器已显示公网入口，后台展示更清晰。"})
	}

	realityNodes := 0
	socksNodes := 0
	socksNoAuth := []string{}
	for _, n := range r.store.Data.Nodes {
		if n.Enabled && strings.ToLower(n.Security) == "reality" {
			realityNodes++
		}
		if n.Enabled && strings.ToLower(n.Protocol) == "socks" {
			socksNodes++
			if strings.TrimSpace(n.SocksUsername) == "" || strings.TrimSpace(n.SocksPassword) == "" {
				socksNoAuth = append(socksNoAuth, n.Name)
			}
		}
	}
	if len(r.store.Data.Nodes) == 0 {
		checks = append(checks, systemCheck{Key: "nodes", Label: "节点配置", Status: "warn", Message: "还没有节点，请先添加 VLESS 测试节点或 Reality 推荐节点。"})
	} else {
		checks = append(checks, systemCheck{Key: "nodes", Label: "节点配置", Status: "ok", Message: "已创建节点。"})
	}
	if realityNodes > 0 {
		checks = append(checks, systemCheck{Key: "reality", Label: "Reality 推荐模式", Status: "ok", Message: "已存在 Reality 入站，订阅链接会携带 pbk/sid/fp/spx 参数。"})
	} else if len(r.store.Data.Nodes) > 0 {
		checks = append(checks, systemCheck{Key: "reality", Label: "Reality 推荐模式", Status: "warn", Message: "当前入站仍为基础模式。测试可以继续使用，正式使用建议新增 Reality 入站。"})
	}

	if socksNodes > 0 {
		if len(socksNoAuth) > 0 {
			checks = append(checks, systemCheck{Key: "socks5_auth", Label: "SOCKS5 安全", Status: "fail", Message: "检测到 SOCKS5 入站未设置账号密码：" + strings.Join(socksNoAuth, "、") + "。请补充账号密码，避免落地出口裸奔。"})
		} else {
			checks = append(checks, systemCheck{Key: "socks5_inbound", Label: "SOCKS5 落地入站", Status: "ok", Message: fmt.Sprintf("已创建 %d 个 SOCKS5 落地入站。建议安全组只允许中转服务器 IP 访问对应端口。", socksNodes)})
		}
	}

	enabledClients := 0
	for _, c := range r.store.Data.Clients {
		if c.Enabled && (c.ExpireAt.IsZero() || c.ExpireAt.After(now)) {
			enabledClients++
		}
	}
	fixedExitClients := 0
	for _, c := range r.store.Data.Clients {
		if c.Enabled && len(c.RelayRouteIDs) > 0 {
			fixedExitClients++
		}
	}
	if enabledClients == 0 {
		checks = append(checks, systemCheck{Key: "clients", Label: "客户 UUID", Status: "warn", Message: "没有可用客户，Xray clients 可能为空。"})
	} else {
		checks = append(checks, systemCheck{Key: "clients", Label: "客户 UUID", Status: "ok", Message: fmt.Sprintf("存在 %d 个可用客户，其中 %d 个客户已绑定固定出口中转线路。", enabledClients, fixedExitClients)})
	}
	if len(r.store.Data.LandingExits) == 0 {
		checks = append(checks, systemCheck{Key: "landing_exits", Label: "落地出口库", Status: "warn", Message: "还没有保存落地出口。50 个出口 IP 场景建议先到落地出口管理批量导入。"})
	} else {
		checks = append(checks, systemCheck{Key: "landing_exits", Label: "落地出口库", Status: "ok", Message: fmt.Sprintf("已保存 %d 个落地 SOCKS5 出口，可在客户管理中直接绑定固定出口。", len(r.store.Data.LandingExits))})
	}

	portMap := map[string]string{}
	conflict := ""
	for _, n := range r.store.Data.Nodes {
		if !n.Enabled {
			continue
		}
		key := n.ServerID + ":" + strconv.Itoa(n.Port)
		if old, ok := portMap[key]; ok {
			conflict = old + " 与入站 " + n.Name + " 使用了相同端口 " + strconv.Itoa(n.Port)
			break
		}
		portMap[key] = "入站 " + n.Name
	}
	for _, rr := range r.store.Data.RelayRoutes {
		if !rr.Enabled {
			continue
		}
		key := rr.RelayServerID + ":" + strconv.Itoa(rr.RelayPort)
		if old, ok := portMap[key]; ok {
			conflict = old + " 与中转线路 " + rr.Name + " 使用了相同端口 " + strconv.Itoa(rr.RelayPort)
			break
		}
		portMap[key] = "中转线路 " + rr.Name
	}
	if conflict != "" {
		checks = append(checks, systemCheck{Key: "ports", Label: "端口冲突", Status: "warn", Message: conflict})
	} else {
		checks = append(checks, systemCheck{Key: "ports", Label: "端口冲突", Status: "ok", Message: "未发现同服务器端口冲突。"})
	}

	lowPortNames := []string{}
	for _, n := range r.store.Data.Nodes {
		if n.Enabled && n.Port > 0 && n.Port < 10000 {
			lowPortNames = append(lowPortNames, fmt.Sprintf("入站 %s:%d", n.Name, n.Port))
		}
	}
	for _, rr := range r.store.Data.RelayRoutes {
		if rr.Enabled && rr.RelayPort > 0 && rr.RelayPort < 10000 {
			lowPortNames = append(lowPortNames, fmt.Sprintf("中转 %s:%d", rr.Name, rr.RelayPort))
		}
	}
	if len(lowPortNames) > 0 {
		checks = append(checks, systemCheck{Key: "port_range", Label: "节点端口建议", Status: "warn", Message: "检测到低于 10000 的端口：" + strings.Join(lowPortNames, "、") + "。部分服务器商或机房会限制低端口外部访问，建议改用 10000-60000。"})
	} else if len(r.store.Data.Nodes) > 0 || len(r.store.Data.RelayRoutes) > 0 {
		checks = append(checks, systemCheck{Key: "port_range", Label: "节点端口建议", Status: "ok", Message: "当前入站/中转端口位于推荐范围，继续创建新端口时建议使用 10000-60000。"})
	}

	if len(r.store.Data.RelayRoutes) == 0 {
		checks = append(checks, systemCheck{Key: "relay_routes", Label: "中转线路", Status: "warn", Message: "暂未创建中转线路。TCP 直连可继续使用；需要中转时请到中转管理新增线路。"})
	} else {
		checks = append(checks, systemCheck{Key: "relay_routes", Label: "中转线路", Status: "ok", Message: fmt.Sprintf("已创建 %d 条中转线路，一台中转服务器可以绑定多条落地线路。", len(r.store.Data.RelayRoutes))})
	}
	relayOffline := []string{}
	relayTCPUDP := []string{}
	relayUDPOnlyInvalid := []string{}
	relaySocksRouteNames := []string{}
	relaySocksRouteBroken := []string{}
	relayTCPForwardNames := []string{}
	for _, rr := range r.store.Data.RelayRoutes {
		if !rr.Enabled {
			continue
		}
		srv, ok := r.store.Data.Servers[rr.RelayServerID]
		if !ok || srv.Status != "online" {
			relayOffline = append(relayOffline, rr.Name)
		}
		switch relayRouteMode(rr.RouteMode) {
		case "socks5_route":
			relaySocksRouteNames = append(relaySocksRouteNames, rr.Name)
			landingMode := strings.ToLower(strings.TrimSpace(rr.LandingMode))
			if landingMode == "" && rr.LandingNodeID != "" {
				landingMode = "panel_node"
			}
			if landingMode == "manual_socks5" {
				if strings.TrimSpace(rr.ManualSocksHost) == "" || rr.ManualSocksPort <= 0 || strings.TrimSpace(rr.ManualSocksUsername) == "" || strings.TrimSpace(rr.ManualSocksPassword) == "" {
					relaySocksRouteBroken = append(relaySocksRouteBroken, rr.Name+"：手动 SOCKS5 参数不完整")
				}
			} else {
				landing, ok := r.store.Data.Nodes[rr.LandingNodeID]
				if !ok {
					relaySocksRouteBroken = append(relaySocksRouteBroken, rr.Name+"：落地节点不存在")
				} else if strings.ToLower(strings.TrimSpace(landing.Protocol)) != "socks" {
					relaySocksRouteBroken = append(relaySocksRouteBroken, rr.Name+"：未绑定 SOCKS5 落地")
				}
			}
			if strings.TrimSpace(rr.RelayRealityPrivateKey) == "" || strings.TrimSpace(rr.RelayRealityPublicKey) == "" {
				relaySocksRouteBroken = append(relaySocksRouteBroken, rr.Name+"：缺少中转 Reality 密钥")
			}
		case "tcp_forward":
			relayTCPForwardNames = append(relayTCPForwardNames, rr.Name)
			landing, ok := r.store.Data.Nodes[rr.LandingNodeID]
			if !ok {
				relaySocksRouteBroken = append(relaySocksRouteBroken, rr.Name+"：落地节点不存在")
				continue
			}
			network := relayNetworkOrTCP(rr.RelayNetwork)
			if isVLESSRealityTCP(landing) && network == "udp" {
				relayUDPOnlyInvalid = append(relayUDPOnlyInvalid, rr.Name)
			}
			if isVLESSRealityTCP(landing) && network == "tcp,udp" {
				relayTCPUDP = append(relayTCPUDP, rr.Name)
			}
		}
	}
	if len(relayOffline) > 0 {
		checks = append(checks, systemCheck{Key: "relay_server_online", Label: "中转服务器在线", Status: "warn", Message: "以下中转线路的中转服务器未在线：" + strings.Join(relayOffline, "、") + "。"})
	} else if len(r.store.Data.RelayRoutes) > 0 {
		checks = append(checks, systemCheck{Key: "relay_server_online", Label: "中转服务器在线", Status: "ok", Message: "已启用中转线路的服务器均处于在线或已接入状态。"})
	}
	if len(relaySocksRouteBroken) > 0 {
		checks = append(checks, systemCheck{Key: "socks5_route", Label: "SOCKS5 路由中转", Status: "fail", Message: "SOCKS5 路由中转配置不完整：" + strings.Join(relaySocksRouteBroken, "、") + "。"})
	} else if len(relaySocksRouteNames) > 0 {
		checks = append(checks, systemCheck{Key: "socks5_route", Label: "SOCKS5 路由中转", Status: "ok", Message: "已创建 SOCKS5 路由中转：" + strings.Join(relaySocksRouteNames, "、") + "。中转服务器会生成 VLESS Reality 入站、SOCKS5 出站和 routing 绑定。"})
	}
	if len(relayUDPOnlyInvalid) > 0 {
		checks = append(checks, systemCheck{Key: "relay_udp_only", Label: "UDP-only 中转", Status: "fail", Message: "UDP-only 不能绑定 VLESS Reality TCP 落地节点，请改为 TCP。涉及线路：" + strings.Join(relayUDPOnlyInvalid, "、") + "。"})
	} else if len(relayTCPUDP) > 0 {
		checks = append(checks, systemCheck{Key: "relay_tcp_udp", Label: "TCP+UDP 实验模式", Status: "warn", Message: "检测到 VLESS Reality 线路使用 TCP+UDP，可能变慢；正式使用建议改为 TCP。涉及线路：" + strings.Join(relayTCPUDP, "、") + "。"})
	} else if len(relayTCPForwardNames) > 0 {
		checks = append(checks, systemCheck{Key: "relay_protocol", Label: "TCP 透传中转", Status: "ok", Message: "TCP 透传中转协议与落地节点类型匹配。VLESS Reality 建议使用 TCP 中转。"})
	}

	nodes := make([]model.Node, 0, len(r.store.Data.Nodes))
	for _, n := range r.store.Data.Nodes {
		nodes = append(nodes, n)
	}
	clients := make([]model.Client, 0, len(r.store.Data.Clients))
	for _, c := range r.store.Data.Clients {
		clients = append(clients, c)
	}
	relays := make([]model.RelayRoute, 0, len(r.store.Data.RelayRoutes))
	for _, rr := range r.store.Data.RelayRoutes {
		relays = append(relays, rr)
	}
	desired := xray.GenerateServerConfig(nodes, clients, relays, r.store.Data.Nodes, r.store.Data.NetworkPolicy)
	if len(relaySocksRouteNames) > 0 {
		missingBindings := socks5RouteBindingProblems(desired, r.store.Data.RelayRoutes)
		if len(missingBindings) > 0 {
			checks = append(checks, systemCheck{Key: "socks5_route_binding", Label: "SOCKS5 routing 绑定", Status: "fail", Message: "以下 SOCKS5 路由中转未在目标 Xray 配置中生成完整入站/出站/routing：" + strings.Join(missingBindings, "、") + "。"})
		} else {
			checks = append(checks, systemCheck{Key: "socks5_route_binding", Label: "SOCKS5 routing 绑定", Status: "ok", Message: "目标 Xray 配置已生成 SOCKS5 路由中转的 VLESS 入站、SOCKS5 出站和 routing 绑定。"})
		}
	}

	policy := r.store.Data.NetworkPolicy
	if hasDNSProtection(desired) {
		checks = append(checks, systemCheck{Key: "dns_protection", Label: "DNS 策略", Status: "ok", Message: "当前网络策略已生成公共 DNS 与 UseIPv4；未强制阻断 53，不会影响主链路速度。"})
	} else {
		checks = append(checks, systemCheck{Key: "dns_protection", Label: "DNS 策略", Status: "warn", Message: "当前策略未启用公共 DNS。若检测到 DNS 漂移，可到 高级：网络策略 手动启用公共 DNS 稳定模式。"})
	}
	if policy.BlockDNS53 || policy.BlockQUIC || policy.DisableFallback {
		checks = append(checks, systemCheck{Key: "network_policy_strict", Label: "网络策略强度", Status: "warn", Message: "当前启用了严格网络策略选项，可能导致部分环境变慢。若出现卡顿，请到 高级：网络策略 切回兼容稳定模式。"})
	} else {
		checks = append(checks, systemCheck{Key: "network_policy_safe", Label: "网络策略强度", Status: "ok", Message: "当前未启用 53 端口阻断、QUIC 阻断或禁用 fallback，属于兼容稳定策略。"})
	}
	if hasSniffing(desired) {
		checks = append(checks, systemCheck{Key: "sniffing", Label: "流量嗅探", Status: "ok", Message: "所有目标 inbound 已开启 http/tls/quic sniffing。"})
	} else if len(r.store.Data.Nodes) > 0 {
		checks = append(checks, systemCheck{Key: "sniffing", Label: "流量嗅探", Status: "warn", Message: "部分目标 inbound 未开启 sniffing。"})
	}
	if hasFreedomUseIPv4(desired) {
		checks = append(checks, systemCheck{Key: "outbound_strategy", Label: "出站策略", Status: "ok", Message: "freedom outbound 已设置 domainStrategy=UseIPv4。"})
	} else {
		checks = append(checks, systemCheck{Key: "outbound_strategy", Label: "出站策略", Status: "warn", Message: "freedom outbound 未设置 UseIPv4，可能出现 DNS/IPv6 旁路。"})
	}

	return checks, map[string]int{"servers": len(r.store.Data.Servers), "nodes": len(r.store.Data.Nodes), "clients": len(r.store.Data.Clients), "landing_exits": len(r.store.Data.LandingExits), "relays": len(r.store.Data.RelayRoutes), "online_servers": onlineServers, "socks_nodes": socksNodes, "socks5_route_relays": len(relaySocksRouteNames)}
}

func relayRouteMode(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "tcp_forward"
	}
	return v
}

func relayNetworkOrTCP(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "tcp"
	}
	return v
}

func isVLESSRealityTCP(n model.Node) bool {
	transport := strings.ToLower(strings.TrimSpace(n.Transport))
	return strings.ToLower(strings.TrimSpace(n.Protocol)) == "vless" && strings.ToLower(strings.TrimSpace(n.Security)) == "reality" && (transport == "" || transport == "tcp")
}

func hasDNSProtection(cfg map[string]any) bool {
	dns, ok := cfg["dns"].(map[string]any)
	if !ok {
		return false
	}
	if dns["queryStrategy"] != "UseIPv4" {
		return false
	}
	servers, ok := dns["servers"].([]any)
	if !ok || len(servers) == 0 {
		return false
	}
	return true
}

func hasSniffing(cfg map[string]any) bool {
	inbounds, ok := cfg["inbounds"].([]any)
	if !ok || len(inbounds) == 0 {
		return false
	}
	for _, item := range inbounds {
		inbound, ok := item.(map[string]any)
		if !ok {
			return false
		}
		sniff, ok := inbound["sniffing"].(map[string]any)
		if !ok || sniff["enabled"] != true {
			return false
		}
	}
	return true
}

func hasFreedomUseIPv4(cfg map[string]any) bool {
	outbounds, ok := cfg["outbounds"].([]any)
	if !ok {
		return false
	}
	for _, item := range outbounds {
		out, ok := item.(map[string]any)
		if !ok || out["protocol"] != "freedom" {
			continue
		}
		settings, ok := out["settings"].(map[string]any)
		return ok && settings["domainStrategy"] == "UseIPv4"
	}
	return false
}

func socks5RouteBindingProblems(cfg map[string]any, relays map[string]model.RelayRoute) []string {
	inboundTags := map[string]bool{}
	outboundTags := map[string]bool{}
	routeBindings := map[string]string{}
	if items, ok := cfg["inbounds"].([]any); ok {
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if tag, ok := m["tag"].(string); ok {
				inboundTags[tag] = true
			}
		}
	}
	if items, ok := cfg["outbounds"].([]any); ok {
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if tag, ok := m["tag"].(string); ok {
				outboundTags[tag] = true
			}
		}
	}
	if routing, ok := cfg["routing"].(map[string]any); ok {
		if rules, ok := routing["rules"].([]any); ok {
			for _, item := range rules {
				rule, ok := item.(map[string]any)
				if !ok {
					continue
				}
				outTag, _ := rule["outboundTag"].(string)
				switch tags := rule["inboundTag"].(type) {
				case []any:
					for _, t := range tags {
						if tag, ok := t.(string); ok {
							routeBindings[tag] = outTag
						}
					}
				case []string:
					for _, tag := range tags {
						routeBindings[tag] = outTag
					}
				}
			}
		}
	}
	problems := []string{}
	for _, rr := range relays {
		if !rr.Enabled || relayRouteMode(rr.RouteMode) != "socks5_route" {
			continue
		}
		inTag := "in_" + rr.ID
		outTag := "out_" + rr.ID
		missing := []string{}
		if !inboundTags[inTag] {
			missing = append(missing, "入站")
		}
		if !outboundTags[outTag] {
			missing = append(missing, "出站")
		}
		if routeBindings[inTag] != outTag {
			missing = append(missing, "routing")
		}
		if len(missing) > 0 {
			problems = append(problems, rr.Name+" 缺少"+strings.Join(missing, "/"))
		}
	}
	return problems
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func timeOrDash(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func isLoopbackForReport(v string) bool {
	switch strings.TrimSpace(v) {
	case "127.0.0.1", "localhost", "::1", "0.0.0.0":
		return true
	default:
		return false
	}
}
