// SPDX-License-Identifier: AGPL-3.0-only
package xray

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"zxy-panel/backend/internal/model"
)

// GenerateInboundConfig 生成单个 inbound 配置片段，供面板预览使用。
func GenerateInboundConfig(node model.Node, clients []model.Client) map[string]any {
	return GenerateServerConfig([]model.Node{node}, clients, nil, nil)
}

// GenerateServerConfig 根据某台服务器下的节点和客户绑定关系生成完整 Xray 配置。
// V0.5.8 默认加入 DNS 防泄漏、sniffing 和 freedom outbound domainStrategy，避免客户端全局代理下仍出现本地 DNS 泄漏。
func GenerateServerConfig(nodes []model.Node, clients []model.Client, relayRoutes []model.RelayRoute, allNodes map[string]model.Node) map[string]any {
	inbounds := make([]any, 0)
	outbounds := []any{
		map[string]any{
			"protocol": "freedom",
			"tag":      "direct",
			"settings": map[string]any{"domainStrategy": "UseIPv4"},
		},
		map[string]any{"protocol": "blackhole", "tag": "blocked"},
	}
	rules := make([]any, 0)

	for _, node := range nodes {
		if !node.Enabled {
			continue
		}
		if node.Protocol == "socks" {
			auth := "password"
			accounts := []any{map[string]any{"user": node.SocksUsername, "pass": node.SocksPassword}}
			if node.SocksUsername == "" || node.SocksPassword == "" {
				auth = "noauth"
				accounts = []any{}
			}
			inbounds = append(inbounds, map[string]any{
				"listen":   "0.0.0.0",
				"port":     node.Port,
				"protocol": "socks",
				"settings": map[string]any{
					"auth":     auth,
					"accounts": accounts,
					"udp":      node.SocksUDP,
					"ip":       "127.0.0.1",
				},
				"sniffing": map[string]any{
					"enabled":      true,
					"destOverride": []any{"http", "tls", "quic"},
					"routeOnly":    false,
				},
				"tag": node.ID,
			})
			continue
		}

		boundClients := makeVlessClients(clients, node.ID)
		inbound := map[string]any{
			"listen":   "0.0.0.0",
			"port":     node.Port,
			"protocol": node.Protocol,
			"settings": map[string]any{
				"clients":    boundClients,
				"decryption": "none",
			},
			"sniffing": map[string]any{
				"enabled":      true,
				"destOverride": []any{"http", "tls", "quic"},
				"routeOnly":    false,
			},
			"streamSettings": map[string]any{"network": node.Transport, "security": node.Security},
			"tag":            node.ID,
		}
		s := inbound["streamSettings"].(map[string]any)
		switch node.Transport {
		case "ws":
			s["wsSettings"] = map[string]any{"path": node.Path}
		case "grpc":
			serviceName := node.Path
			if serviceName == "" || serviceName == "/" {
				serviceName = "zxy"
			}
			s["grpcSettings"] = map[string]any{"serviceName": trimSlash(serviceName)}
		}
		if node.Security == "tls" {
			s["tlsSettings"] = map[string]any{"serverName": node.SNI}
		}
		if node.Security == "reality" {
			applyRealitySettings(s, node.RealityDest, node.SNI, node.RealityPrivateKey, node.RealityShortID, node.RealitySpiderX)
		}
		inbounds = append(inbounds, inbound)
	}

	for _, relay := range relayRoutes {
		if !relay.Enabled || relay.RelayPort <= 0 {
			continue
		}
		routeMode := relay.RouteMode
		if routeMode == "" {
			routeMode = "tcp_forward"
		}
		switch routeMode {
		case "socks5_route":
			targetHost := ""
			targetPort := 0
			socksUser := ""
			socksPass := ""
			landingMode := relay.LandingMode
			if landingMode == "" {
				if relay.LandingNodeID != "" {
					landingMode = "panel_node"
				} else {
					landingMode = "manual_socks5"
				}
			}
			if landingMode == "manual_socks5" {
				targetHost = relay.ManualSocksHost
				targetPort = relay.ManualSocksPort
				socksUser = relay.ManualSocksUsername
				socksPass = relay.ManualSocksPassword
			} else {
				landing, ok := allNodes[relay.LandingNodeID]
				if !ok || !landing.Enabled || landing.Port <= 0 || landing.Protocol != "socks" {
					continue
				}
				targetHost = landing.Host
				if targetHost == "" {
					targetHost = "127.0.0.1"
				}
				targetPort = landing.Port
				socksUser = landing.SocksUsername
				socksPass = landing.SocksPassword
			}
			if targetHost == "" || targetPort <= 0 || socksUser == "" || socksPass == "" {
				continue
			}
			inboundTag := "in_" + relay.ID
			outboundTag := "out_" + relay.ID
			inbound := map[string]any{
				"listen":   "0.0.0.0",
				"port":     relay.RelayPort,
				"protocol": "vless",
				"settings": map[string]any{
					"clients":    makeRelayVlessClients(clients, relay.ID),
					"decryption": "none",
				},
				"sniffing": map[string]any{
					"enabled":      true,
					"destOverride": []any{"http", "tls", "quic"},
					"routeOnly":    false,
				},
				"streamSettings": map[string]any{"network": "tcp", "security": "reality"},
				"tag":            inboundTag,
			}
			stream := inbound["streamSettings"].(map[string]any)
			applyRealitySettings(stream, relay.RelayRealityDest, relay.RelaySNI, relay.RelayRealityPrivateKey, relay.RelayRealityShortID, relay.RelayRealitySpiderX)
			inbounds = append(inbounds, inbound)

			user := map[string]any{"user": socksUser, "pass": socksPass}
			outbounds = append(outbounds, map[string]any{
				"protocol": "socks",
				"tag":      outboundTag,
				"settings": map[string]any{
					"servers": []any{map[string]any{
						"address": targetHost,
						"port":    targetPort,
						"users":   []any{user},
					}},
				},
			})
			rules = append(rules, map[string]any{
				"type":        "field",
				"inboundTag":  []any{inboundTag},
				"outboundTag": outboundTag,
			})
		default:
			landing, ok := allNodes[relay.LandingNodeID]
			if !ok || !landing.Enabled || landing.Port <= 0 {
				continue
			}
			targetHost := landing.Host
			if targetHost == "" {
				targetHost = "127.0.0.1"
			}
			relayNetwork := relay.RelayNetwork
			if relayNetwork == "" {
				relayNetwork = "tcp"
			}
			inbounds = append(inbounds, map[string]any{
				"listen":   "0.0.0.0",
				"port":     relay.RelayPort,
				"protocol": "dokodemo-door",
				"settings": map[string]any{
					"address": targetHost,
					"port":    landing.Port,
					"network": relayNetwork,
				},
				"sniffing": map[string]any{
					"enabled":      true,
					"destOverride": []any{"http", "tls", "quic"},
					"routeOnly":    false,
				},
				"tag": relay.ID,
			})
		}
	}
	return map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"dns": map[string]any{
			"servers":       []any{"1.1.1.1", "8.8.8.8", "9.9.9.9"},
			"queryStrategy": "UseIPv4",
		},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing":   map[string]any{"domainStrategy": "IPIfNonMatch", "rules": rules},
	}
}

func makeRelayVlessClients(clients []model.Client, relayID string) []map[string]any {
	boundClients := make([]map[string]any, 0)
	for _, c := range clients {
		if !c.Enabled || (!c.ExpireAt.IsZero() && c.ExpireAt.Before(time.Now())) || c.UUID == "" {
			continue
		}
		// V0.7.5：固定出口中转线路只写入显式绑定该线路的客户 UUID。
		// 不再把未绑定 relay_route_ids 的旧客户自动塞进中转入口，避免多个出口串线或误用。
		if contains(c.RelayRouteIDs, relayID) {
			boundClients = append(boundClients, map[string]any{"id": c.UUID, "email": c.Username, "flow": ""})
		}
	}
	return boundClients
}

func makeVlessClients(clients []model.Client, nodeID string) []map[string]any {
	boundClients := make([]map[string]any, 0)
	for _, c := range clients {
		if !c.Enabled || (!c.ExpireAt.IsZero() && c.ExpireAt.Before(time.Now())) {
			continue
		}
		// nodeID 为空时用于中转线路：中转节点是虚拟线路，不写入客户 NodeIDs，所有有效客户都可用于复制中转链接。
		if nodeID != "" && len(c.NodeIDs) > 0 && !contains(c.NodeIDs, nodeID) {
			continue
		}
		if c.UUID == "" {
			continue
		}
		boundClients = append(boundClients, map[string]any{"id": c.UUID, "email": c.Username, "flow": ""})
	}
	return boundClients
}

func applyRealitySettings(stream map[string]any, dest, serverName, privateKey, shortID, spiderX string) {
	if dest == "" {
		dest = "www.intel.com:443"
	}
	if serverName == "" {
		serverName = "www.intel.com"
	}
	if spiderX == "" {
		spiderX = "/"
	}
	stream["realitySettings"] = map[string]any{
		"show":        false,
		"dest":        dest,
		"xver":        0,
		"serverNames": []any{serverName},
		"privateKey":  privateKey,
		"shortIds":    []any{shortID},
		"spiderX":     spiderX,
	}
}

func ConfigHash(cfg map[string]any) string {
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func trimSlash(s string) string {
	for len(s) > 0 && s[0] == '/' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	if s == "" {
		return "zxy"
	}
	return s
}
