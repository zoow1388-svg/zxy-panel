// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"zxy-panel/backend/internal/model"
)

type diagnosisItem struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

type diagnosisPort struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Server  string `json:"server"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type nodeDiagnosisResponse struct {
	Version         string          `json:"version"`
	CheckedAt       string          `json:"checked_at"`
	Score           int             `json:"score"`
	Summary         string          `json:"summary"`
	PanelPort       string          `json:"panel_port"`
	WebBasePath     string          `json:"web_base_path"`
	InstallMode     string          `json:"install_mode"`
	Counts          map[string]int  `json:"counts"`
	RuntimeChecks   []diagnosisItem `json:"runtime_checks"`
	NetworkChecks   []diagnosisItem `json:"network_checks"`
	ConfigChecks    []diagnosisItem `json:"config_checks"`
	PortChecks      []diagnosisPort `json:"port_checks"`
	Recommendations []string        `json:"recommendations"`
}

func (r *Router) nodeDiagnosisRun(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	resp := r.buildNodeDiagnosis()
	writeJSON(w, http.StatusOK, resp)
}

func (r *Router) nodeDiagnosisReport(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	resp := r.buildNodeDiagnosis()
	text := renderNodeDiagnosisReport(resp)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(text))
}

func (r *Router) buildNodeDiagnosis() nodeDiagnosisResponse {
	panelInfo := readKVFile("/etc/zxy-panel/panel.info")
	envInfo := readKVFile("/opt/zxy-panel/.env")
	panelPort := firstNonEmptyDiag(panelInfo["PORT"], envInfo["ZXY_PANEL_PORT"])
	webBasePath := firstNonEmptyDiag(panelInfo["WEB_BASE_PATH"], envInfo["ZXY_WEB_BASE_PATH"])
	installMode := firstNonEmptyDiag(panelInfo["INSTALL_MODE"], envInfo["ZXY_INSTALL_MODE"])
	if installMode == "" {
		installMode = "unknown"
	}

	runtime := []diagnosisItem{}
	network := []diagnosisItem{}
	config := []diagnosisItem{}
	ports := []diagnosisPort{}
	recommendations := []string{}

	runtime = append(runtime, serviceCheck("zxy-panel-api", "API 服务"))
	runtime = append(runtime, serviceCheck("zxy-agent", "Agent 服务"))
	runtime = append(runtime, serviceCheck("xray", "Xray 网络核心"))
	runtime = append(runtime, serviceCheck("nginx", "Nginx 面板入口"))

	if canTCPDial("127.0.0.1", 8088, 1200*time.Millisecond) {
		runtime = append(runtime, diagnosisItem{Key: "api_port", Label: "API 本机端口", Status: "ok", Message: "127.0.0.1:8088 正在监听。"})
	} else {
		runtime = append(runtime, diagnosisItem{Key: "api_port", Label: "API 本机端口", Status: "fail", Message: "127.0.0.1:8088 无法连接，请检查 zxy-panel-api 服务。"})
		recommendations = append(recommendations, "执行 systemctl status zxy-panel-api --no-pager -l 查看 API 服务状态。")
	}

	if panelPort != "" {
		if p, err := strconv.Atoi(panelPort); err == nil && canTCPDial("127.0.0.1", p, 1200*time.Millisecond) {
			runtime = append(runtime, diagnosisItem{Key: "panel_port", Label: "面板入口端口", Status: "ok", Message: "本机面板端口 " + panelPort + " 正在监听。"})
		} else {
			runtime = append(runtime, diagnosisItem{Key: "panel_port", Label: "面板入口端口", Status: "fail", Message: "本机面板端口 " + panelPort + " 无法连接，请检查 Nginx 配置。"})
			recommendations = append(recommendations, "检查云服务器安全组是否放行面板端口 "+panelPort+"，并执行 nginx -t。")
		}
	} else {
		runtime = append(runtime, diagnosisItem{Key: "panel_port_missing", Label: "面板入口端口", Status: "warn", Message: "未在 /etc/zxy-panel/panel.info 中读取到 PORT。"})
	}

	nginxConf, err := os.ReadFile("/etc/nginx/conf.d/zxy-panel.conf")
	if err != nil {
		config = append(config, diagnosisItem{Key: "nginx_conf", Label: "Nginx 配置文件", Status: "warn", Message: "未读取到 /etc/nginx/conf.d/zxy-panel.conf。", Detail: err.Error()})
		recommendations = append(recommendations, "fast 模式需要宿主机 Nginx 托管前端并反代 /api/、/sub/、/s/。")
	} else {
		txt := string(nginxConf)
		missing := []string{}
		for _, need := range []string{"location ^~ /api/", "location ^~ /sub/", "location ^~ /s/"} {
			if !strings.Contains(txt, need) {
				missing = append(missing, need)
			}
		}
		if len(missing) == 0 {
			config = append(config, diagnosisItem{Key: "nginx_routes", Label: "Nginx 反代路径", Status: "ok", Message: "已包含 /api/、/sub/、/s/ 根路径反代，登录和订阅接口可正常转发。"})
		} else {
			config = append(config, diagnosisItem{Key: "nginx_routes", Label: "Nginx 反代路径", Status: "fail", Message: "缺少关键反代路径：" + strings.Join(missing, "、") + "。"})
			recommendations = append(recommendations, "重新运行 V0.7.5.8 安装脚本，或修复 /etc/nginx/conf.d/zxy-panel.conf。")
		}
		if webBasePath != "" && strings.Contains(txt, "/"+webBasePath+"/") {
			config = append(config, diagnosisItem{Key: "web_base_path", Label: "随机后台路径", Status: "ok", Message: "Nginx 已包含当前 WebBasePath：/" + webBasePath + "/。"})
		} else if webBasePath != "" {
			config = append(config, diagnosisItem{Key: "web_base_path", Label: "随机后台路径", Status: "warn", Message: "Nginx 配置中未明显匹配当前 WebBasePath：/" + webBasePath + "/。"})
		}
	}

	xrayConfigPath := firstExistingFile("/etc/zxy-panel/xray/config.json", "/usr/local/etc/xray/config.json")
	if xrayConfigPath != "" {
		if st, err := os.Stat(xrayConfigPath); err == nil && st.Size() > 20 {
			config = append(config, diagnosisItem{Key: "xray_config", Label: "Xray 配置文件", Status: "ok", Message: "已检测到 Xray 配置：" + xrayConfigPath})
		} else {
			config = append(config, diagnosisItem{Key: "xray_config", Label: "Xray 配置文件", Status: "warn", Message: "Xray 配置文件存在但内容异常：" + xrayConfigPath})
		}
	} else {
		config = append(config, diagnosisItem{Key: "xray_config", Label: "Xray 配置文件", Status: "warn", Message: "未检测到常见 Xray 配置路径。"})
	}

	resolvText, _ := os.ReadFile("/etc/resolv.conf")
	chinaDNS := findChinaDNS(string(resolvText))
	if len(chinaDNS) > 0 {
		network = append(network, diagnosisItem{Key: "dns_china", Label: "系统 DNS 风险", Status: "warn", Message: "resolv.conf 中出现中国公共 DNS：" + strings.Join(chinaDNS, "、") + "。软路由或本机直连场景可能造成识别漂移。"})
		recommendations = append(recommendations, "如遇 Gemini/Google 地区识别异常，可到 高级：网络策略 启用公共 DNS 稳定模式。")
	} else {
		network = append(network, diagnosisItem{Key: "dns_china", Label: "系统 DNS 风险", Status: "ok", Message: "未在 /etc/resolv.conf 中发现常见中国公共 DNS。"})
	}

	if ipv6Enabled() {
		network = append(network, diagnosisItem{Key: "ipv6", Label: "IPv6 状态", Status: "warn", Message: "系统 IPv6 当前未禁用。若客户端或软路由存在 IPv6 旁路，可能导致地区识别异常。"})
	} else {
		network = append(network, diagnosisItem{Key: "ipv6", Label: "IPv6 状态", Status: "ok", Message: "系统 IPv6 显示为禁用或不可用。"})
	}

	publicIP := detectPublicIPv4()
	if publicIP != "" {
		network = append(network, diagnosisItem{Key: "public_ip", Label: "本机出口 IP", Status: "ok", Message: "本机访问公网显示出口 IP：" + publicIP})
	} else {
		network = append(network, diagnosisItem{Key: "public_ip", Label: "本机出口 IP", Status: "warn", Message: "未能在 4 秒内读取本机公网出口 IP。可能是服务器网络、DNS 或外部检测站超时。"})
	}

	r.store.Mu.RLock()
	servers := make(map[string]model.Server, len(r.store.Data.Servers))
	for id, srv := range r.store.Data.Servers {
		servers[id] = srv
	}
	nodes := make([]model.Node, 0, len(r.store.Data.Nodes))
	for _, n := range r.store.Data.Nodes {
		nodes = append(nodes, n)
	}
	relays := make([]model.RelayRoute, 0, len(r.store.Data.RelayRoutes))
	for _, rr := range r.store.Data.RelayRoutes {
		relays = append(relays, rr)
	}
	clients := make([]model.Client, 0, len(r.store.Data.Clients))
	for _, c := range r.store.Data.Clients {
		clients = append(clients, c)
	}
	landingExits := len(r.store.Data.LandingExits)
	policy := r.store.Data.NetworkPolicy
	r.store.Mu.RUnlock()

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].CreatedAt.Before(nodes[j].CreatedAt) })
	sort.Slice(relays, func(i, j int) bool { return relays[i].CreatedAt.Before(relays[j].CreatedAt) })

	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		srv := servers[n.ServerID]
		host := firstNonEmptyDiag(n.Host, srv.Host, srv.IP, "127.0.0.1")
		status, msg := checkManagedPort(host, n.Port, srv)
		ports = append(ports, diagnosisPort{Name: n.Name, Kind: "入站", Host: host, Port: n.Port, Server: firstNonEmptyDiag(srv.Name, n.ServerID), Status: status, Message: msg})
	}
	for _, rr := range relays {
		if !rr.Enabled {
			continue
		}
		srv := servers[rr.RelayServerID]
		host := firstNonEmptyDiag(rr.RelayHost, srv.Host, srv.IP, "127.0.0.1")
		status, msg := checkManagedPort(host, rr.RelayPort, srv)
		ports = append(ports, diagnosisPort{Name: rr.Name, Kind: "中转入口", Host: host, Port: rr.RelayPort, Server: firstNonEmptyDiag(srv.Name, rr.RelayServerID), Status: status, Message: msg})
	}
	if len(ports) == 0 {
		config = append(config, diagnosisItem{Key: "managed_ports", Label: "节点端口", Status: "warn", Message: "当前没有启用的入站或中转入口端口。"})
	} else {
		badPorts := 0
		for _, p := range ports {
			if p.Status != "ok" {
				badPorts++
			}
		}
		if badPorts == 0 {
			config = append(config, diagnosisItem{Key: "managed_ports", Label: "节点端口", Status: "ok", Message: fmt.Sprintf("已检测 %d 个启用端口，本机端口监听正常。", len(ports))})
		} else {
			config = append(config, diagnosisItem{Key: "managed_ports", Label: "节点端口", Status: "warn", Message: fmt.Sprintf("已检测 %d 个启用端口，其中 %d 个需要人工确认安全组或远程 Agent。", len(ports), badPorts)})
		}
	}

	if policy.BlockDNS53 || policy.BlockQUIC || policy.DisableFallback {
		network = append(network, diagnosisItem{Key: "strict_policy", Label: "网络策略强度", Status: "warn", Message: "当前启用了严格网络策略选项，可能影响速度。"})
	} else {
		network = append(network, diagnosisItem{Key: "strict_policy", Label: "网络策略强度", Status: "ok", Message: "当前未启用 53 阻断、QUIC 阻断或禁用 fallback，属于兼容稳定策略。"})
	}

	counts := map[string]int{
		"servers":       len(servers),
		"nodes":         len(nodes),
		"clients":       len(clients),
		"relays":        len(relays),
		"landing_exits": landingExits,
		"ports":         len(ports),
	}

	all := append([]diagnosisItem{}, runtime...)
	all = append(all, network...)
	all = append(all, config...)
	fail, warn := 0, 0
	for _, item := range all {
		switch item.Status {
		case "fail":
			fail++
		case "warn":
			warn++
		}
	}
	for _, p := range ports {
		switch p.Status {
		case "fail":
			fail++
		case "warn":
			warn++
		}
	}
	score := 100 - fail*20 - warn*6
	if score < 0 {
		score = 0
	}
	summary := "节点核心链路体检正常，可以继续创建客户和分享节点。"
	if fail > 0 {
		summary = fmt.Sprintf("发现 %d 个失败项，建议先修复红色项目后再给客户使用。", fail)
	} else if warn > 0 {
		summary = fmt.Sprintf("未发现失败项，但有 %d 个注意项，适合继续测试，正式交付前建议处理。", warn)
	}
	if len(recommendations) == 0 {
		recommendations = append(recommendations, "如客户反馈无法连接，优先核对云服务器安全组是否放行对应入站/中转端口。")
		recommendations = append(recommendations, "如 Google/Gemini 地区异常，先看出口 IP 归属，再看 DNS/IPv6/QUIC。")
	}

	return nodeDiagnosisResponse{
		Version: panelVersion, CheckedAt: time.Now().Format(time.RFC3339), Score: score, Summary: summary,
		PanelPort: panelPort, WebBasePath: webBasePath, InstallMode: installMode, Counts: counts,
		RuntimeChecks: runtime, NetworkChecks: network, ConfigChecks: config, PortChecks: ports, Recommendations: uniqueStrings(recommendations),
	}
}

func renderNodeDiagnosisReport(resp nodeDiagnosisResponse) string {
	var b strings.Builder
	b.WriteString("ZXY Panel 节点诊断报告\n")
	b.WriteString("========================\n")
	b.WriteString("生成时间：" + resp.CheckedAt + "\n")
	b.WriteString("版本：" + resp.Version + "\n")
	b.WriteString("安装模式：" + resp.InstallMode + "\n")
	b.WriteString("面板端口：" + resp.PanelPort + "\n")
	if resp.WebBasePath != "" {
		b.WriteString("后台路径：/" + resp.WebBasePath + "/\n")
	}
	b.WriteString(fmt.Sprintf("体检评分：%d\n", resp.Score))
	b.WriteString("结论：" + resp.Summary + "\n\n")
	writeDiagnosisSection(&b, "一、运行状态", resp.RuntimeChecks)
	writeDiagnosisSection(&b, "二、网络与泄漏风险", resp.NetworkChecks)
	writeDiagnosisSection(&b, "三、配置一致性", resp.ConfigChecks)
	b.WriteString("四、节点端口\n")
	if len(resp.PortChecks) == 0 {
		b.WriteString("- [WARN] 暂无启用端口。\n")
	} else {
		for _, p := range resp.PortChecks {
			b.WriteString(fmt.Sprintf("- [%s] %s %s：%s:%d | server=%s | %s\n", strings.ToUpper(p.Status), p.Kind, p.Name, p.Host, p.Port, p.Server, p.Message))
		}
	}
	b.WriteString("\n五、建议\n")
	for _, s := range resp.Recommendations {
		b.WriteString("- " + s + "\n")
	}
	return b.String()
}

func writeDiagnosisSection(b *strings.Builder, title string, items []diagnosisItem) {
	b.WriteString(title + "\n")
	if len(items) == 0 {
		b.WriteString("- [WARN] 暂无检测项。\n\n")
		return
	}
	for _, item := range items {
		b.WriteString(fmt.Sprintf("- [%s] %s：%s\n", strings.ToUpper(item.Status), item.Label, item.Message))
	}
	b.WriteString("\n")
}

func serviceCheck(name, label string) diagnosisItem {
	ctx, cancel := context.WithTimeout(context.Background(), 1600*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "is-active", name).CombinedOutput()
	status := strings.TrimSpace(string(out))
	if err == nil && status == "active" {
		return diagnosisItem{Key: "svc_" + name, Label: label, Status: "ok", Message: name + " 正在运行。"}
	}
	if ctx.Err() == context.DeadlineExceeded {
		return diagnosisItem{Key: "svc_" + name, Label: label, Status: "warn", Message: "检测 " + name + " 超时。"}
	}
	if status == "" {
		status = errString(err)
	}
	return diagnosisItem{Key: "svc_" + name, Label: label, Status: "warn", Message: name + " 当前状态：" + status + "。"}
}

func canTCPDial(host string, port int, timeout time.Duration) bool {
	if host == "" || port <= 0 || port > 65535 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func checkManagedPort(host string, port int, srv model.Server) (string, string) {
	if port <= 0 || port > 65535 {
		return "fail", "端口为空或不合法。"
	}
	if isLocalEndpoint(host) || isLocalEndpoint(srv.IP) || isLocalEndpoint(srv.Host) || len(strings.TrimSpace(srv.IP+srv.Host)) == 0 {
		if canTCPDial("127.0.0.1", port, 1000*time.Millisecond) || canTCPDial(host, port, 1000*time.Millisecond) {
			return "ok", "本机端口正在监听。"
		}
		return "warn", "本机未检测到监听。若刚创建节点，请等待 Agent 同步；若仍失败，请检查 Xray 和安全组。"
	}
	if srv.Status == "online" {
		return "warn", "远程服务器在线，外部端口连通性需要从公网或 Agent 侧进一步确认。"
	}
	return "warn", "远程服务器未在线，无法确认端口是否监听。"
}

func readKVFile(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), "\"'")
		out[key] = val
	}
	return out
}

func firstExistingFile(paths ...string) string {
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

func firstNonEmptyDiag(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func findChinaDNS(text string) []string {
	known := []string{"114.114.114.114", "114.114.115.115", "223.5.5.5", "223.6.6.6", "119.29.29.29", "180.76.76.76", "1.2.4.8", "210.2.4.8"}
	found := []string{}
	for _, ip := range known {
		if strings.Contains(text, ip) {
			found = append(found, ip)
		}
	}
	return found
}

func ipv6Enabled() bool {
	raw, err := os.ReadFile("/proc/sys/net/ipv6/conf/all/disable_ipv6")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(raw)) == "0"
}

func detectPublicIPv4() string {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "ZXY-Panel/"+panelVersion)
	resp, err := (&http.Client{Timeout: 4 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64), 256)
	if scanner.Scan() {
		ip := strings.TrimSpace(scanner.Text())
		if net.ParseIP(ip) != nil && strings.Contains(ip, ".") {
			return ip
		}
	}
	return ""
}

func isLocalEndpoint(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "" || v == "127.0.0.1" || v == "localhost" || v == "::1" || v == "0.0.0.0"
}

func errString(err error) string {
	if err == nil {
		return "unknown"
	}
	return err.Error()
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
