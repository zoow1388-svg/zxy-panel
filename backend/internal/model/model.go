// SPDX-License-Identifier: AGPL-3.0-only
package model

import "time"

type AdminUser struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	Role         string    `json:"role"`
	Enabled      bool      `json:"enabled"`
	LastLoginIP  string    `json:"last_login_ip"`
	LastLoginAt  time.Time `json:"last_login_at"`
	CreatedAt    time.Time `json:"created_at"`
}

type Server struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	IP              string    `json:"ip"`
	Host            string    `json:"host"`
	Region          string    `json:"region"`
	Provider        string    `json:"provider"`
	Status          string    `json:"status"`
	AgentToken      string    `json:"agent_token"`
	AgentVersion    string    `json:"agent_version"`
	XrayVersion     string    `json:"xray_version"`
	ConfigHash      string    `json:"config_hash"`
	LastSyncAt      time.Time `json:"last_sync_at"`
	LastSyncMessage string    `json:"last_sync_message"`
	CPUUsage        float64   `json:"cpu_usage"`
	MemoryUsage     float64   `json:"memory_usage"`
	DiskUsage       float64   `json:"disk_usage"`
	UploadTotal     int64     `json:"upload_total"`
	DownloadTotal   int64     `json:"download_total"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Node struct {
	ID                string    `json:"id"`
	ServerID          string    `json:"server_id"`
	Name              string    `json:"name"`
	Protocol          string    `json:"protocol"`
	Host              string    `json:"host"`
	Port              int       `json:"port"`
	Transport         string    `json:"transport"`
	Security          string    `json:"security"`
	SNI               string    `json:"sni"`
	Path              string    `json:"path"`
	Fingerprint       string    `json:"fingerprint"`
	RealityDest       string    `json:"reality_dest"`
	RealityPrivateKey string    `json:"reality_private_key"`
	RealityPublicKey  string    `json:"reality_public_key"`
	RealityShortID    string    `json:"reality_short_id"`
	RealitySpiderX    string    `json:"reality_spider_x"`
	SocksUsername     string    `json:"socks_username"`
	SocksPassword     string    `json:"socks_password"`
	SocksUDP          bool      `json:"socks_udp"`
	Remark            string    `json:"remark"`
	Enabled           bool      `json:"enabled"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type LandingExit struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Host          string    `json:"host"`
	Port          int       `json:"port"`
	Username      string    `json:"username"`
	Password      string    `json:"password"`
	UDP           bool      `json:"udp"`
	Region        string    `json:"region"`
	Provider      string    `json:"provider"`
	BandwidthMbps int       `json:"bandwidth_mbps"`
	Remark        string    `json:"remark"`
	Enabled       bool      `json:"enabled"`
	LastTestIP    string    `json:"last_test_ip"`
	LastTestMsg   string    `json:"last_test_msg"`
	LastTestAt    time.Time `json:"last_test_at"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Client struct {
	ID             string    `json:"id"`
	Username       string    `json:"username"`
	Email          string    `json:"email"`
	UUID           string    `json:"uuid"`
	TrafficLimitGB int64     `json:"traffic_limit_gb"`
	TrafficUsedGB  int64     `json:"traffic_used_gb"`
	ExpireAt       time.Time `json:"expire_at"`
	SubscribeToken string    `json:"subscribe_token"`
	Enabled        bool      `json:"enabled"`
	NodeIDs        []string  `json:"node_ids"`
	// RelayRouteIDs 用于客户固定出口绑定：客户只能使用绑定的中转线路，避免一个 UUID 在多条出口线路之间乱跳。
	RelayRouteIDs []string  `json:"relay_route_ids"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// RelayRoute 表示“中转服务器 -> 落地服务器”的线路。
// route_mode=tcp_forward：旧版 TCP 透传，中转 dokodemo-door 直接转发到落地 Reality 入站。
// route_mode=socks5_route：SOCKS5 路由中转，中转 VLESS Reality 入站 -> SOCKS5 出站 -> 落地 SOCKS5 入站。
type RelayRoute struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RelayServerID string `json:"relay_server_id"`
	LandingNodeID string `json:"landing_node_id"`
	RelayHost     string `json:"relay_host"`
	RelayPort     int    `json:"relay_port"`
	// LandingMode 用于 socks5_route：panel_node 表示选择本面板 SOCKS5 入站；manual_socks5 表示手动填写远程 SOCKS5。
	LandingMode         string `json:"landing_mode"`
	ManualSocksHost     string `json:"manual_socks_host"`
	ManualSocksPort     int    `json:"manual_socks_port"`
	ManualSocksUsername string `json:"manual_socks_username"`
	ManualSocksPassword string `json:"manual_socks_password"`
	ManualSocksUDP      bool   `json:"manual_socks_udp"`
	// RouteMode 支持 tcp_forward 和 socks5_route。空值按 tcp_forward 兼容旧数据。
	RouteMode string `json:"route_mode"`
	// RelayNetwork 只用于 tcp_forward 的 dokodemo-door 监听协议：tcp、tcp,udp 或 udp。
	RelayNetwork string `json:"relay_network"`
	// 以下 Reality 字段用于 socks5_route 模式下的“中转服务器 VLESS Reality 入站”。
	RelaySNI               string    `json:"relay_sni"`
	RelayFingerprint       string    `json:"relay_fingerprint"`
	RelayRealityDest       string    `json:"relay_reality_dest"`
	RelayRealityPrivateKey string    `json:"relay_reality_private_key"`
	RelayRealityPublicKey  string    `json:"relay_reality_public_key"`
	RelayRealityShortID    string    `json:"relay_reality_short_id"`
	RelayRealitySpiderX    string    `json:"relay_reality_spider_x"`
	Remark                 string    `json:"remark"`
	Enabled                bool      `json:"enabled"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type NetworkPolicy struct {
	Mode                   string    `json:"mode"`
	PublicDNS              bool      `json:"public_dns"`
	DNSServers             []string  `json:"dns_servers"`
	QueryStrategy          string    `json:"query_strategy"`
	DisableFallback        bool      `json:"disable_fallback"`
	DisableFallbackIfMatch bool      `json:"disable_fallback_if_match"`
	BlockDNS53             bool      `json:"block_dns_53"`
	BlockChinaDNS          bool      `json:"block_china_dns"`
	BlockQUIC              bool      `json:"block_quic"`
	IPv6Strategy           string    `json:"ipv6_strategy"`
	ClashIncludeQuad9      bool      `json:"clash_include_quad9"`
	SingBoxIncludeQuad9    bool      `json:"sing_box_include_quad9"`
	UpdatedAt              time.Time `json:"updated_at"`
	UpdatedBy              string    `json:"updated_by"`
}

type OperationLog struct {
	ID        string    `json:"id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	IP        string    `json:"ip"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

type AgentHeartbeat struct {
	ServerID      string  `json:"server_id"`
	Hostname      string  `json:"hostname"`
	AgentVersion  string  `json:"agent_version"`
	XrayVersion   string  `json:"xray_version"`
	ConfigHash    string  `json:"config_hash"`
	LastMessage   string  `json:"last_message"`
	CPUUsage      float64 `json:"cpu_usage"`
	MemoryUsage   float64 `json:"memory_usage"`
	DiskUsage     float64 `json:"disk_usage"`
	UploadTotal   int64   `json:"upload_total"`
	DownloadTotal int64   `json:"download_total"`
}

type AgentSyncRequest struct {
	ServerID     string `json:"server_id"`
	ConfigHash   string `json:"config_hash"`
	AgentVersion string `json:"agent_version"`
}

type AgentSyncResponse struct {
	OK                  bool           `json:"ok"`
	ServerID            string         `json:"server_id"`
	DesiredConfigHash   string         `json:"desired_config_hash"`
	RestartRequired     bool           `json:"restart_required"`
	XrayConfig          map[string]any `json:"xray_config"`
	NextIntervalSeconds int            `json:"next_interval_seconds"`
	Message             string         `json:"message"`
}

type PanelData struct {
	Version             string                  `json:"version"`
	Admins              map[string]AdminUser    `json:"admins"`
	Servers             map[string]Server       `json:"servers"`
	Nodes               map[string]Node         `json:"nodes"`
	Clients             map[string]Client       `json:"clients"`
	RelayRoutes         map[string]RelayRoute   `json:"relay_routes"`
	LandingExits        map[string]LandingExit  `json:"landing_exits"`
	OperationLogs       map[string]OperationLog `json:"operation_logs"`
	NetworkPolicy       NetworkPolicy           `json:"network_policy"`
	NetworkPolicyBackup NetworkPolicy           `json:"network_policy_backup"`
}
