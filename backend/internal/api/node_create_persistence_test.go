// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"zxy-panel/backend/internal/model"
	"zxy-panel/backend/internal/store"
	"zxy-panel/backend/internal/xray"
)

func nodeCreateAPIFixture(t *testing.T) *store.Store {
	t.Helper()
	s := b2APIFixture(t, true)
	c := s.Data.Clients["client"]
	c.NodeIDs = []string{"node"}
	s.Data.Clients[c.ID] = c
	relay := s.Data.RelayRoutes["relay"]
	relay.LandingNodeID = "node"
	s.Data.RelayRoutes[relay.ID] = relay
	server := s.Data.Servers["srv_a"]
	server.LastSyncAt = server.LastSyncAt.Add(time.Hour)
	s.Data.Servers[server.ID] = server
	s.Data.OperationLogs["existing_log"] = model.OperationLog{ID: "existing_log", Actor: "synthetic", Action: "fixture", CreatedAt: server.CreatedAt}
	s.Mu.Lock()
	err := s.SaveLocked()
	s.Mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	persistDeletionFixture(t, s)
	return s
}

func TestNodeCreateAPIKeepsSuccessfulCreationBehavior(t *testing.T) {
	cases := []struct {
		name, payload string
		check         func(*testing.T, model.Node)
	}{
		{"defaults", `{"name":" Synthetic new ","port":32101,"id":"caller-id","enabled":false,"created_at":"2020-01-01T00:00:00Z"}`, func(t *testing.T, n model.Node) {
			if n.Name != "Synthetic new" || n.ServerID != "srv_a" || n.Protocol != "vless" || n.Transport != "tcp" || n.Security != "none" {
				t.Fatal("existing defaults changed")
			}
		}},
		{"explicit_server", `{"name":"Synthetic new","server_id":"srv_b","port":32101}`, func(t *testing.T, n model.Node) {
			if n.ServerID != "srv_b" {
				t.Fatal("explicit server changed")
			}
		}},
		{"ws_tls", `{"name":"Synthetic new","port":32101,"transport":"ws","security":"tls"}`, func(t *testing.T, n model.Node) {
			if n.Path != "/zxy" || n.SNI != n.Host || n.Transport != "ws" || n.Security != "tls" {
				t.Fatal("existing ws/tls defaults changed")
			}
		}},
		{"socks_defaults", `{"name":"Synthetic new","port":32101,"protocol":"socks5","transport":"ws","security":"reality","path":"/old","sni":"synthetic.invalid","reality_dest":"synthetic.invalid:443"}`, func(t *testing.T, n model.Node) {
			if n.Protocol != "socks" || n.Transport != "tcp" || n.Security != "none" || n.SocksUsername != "zxy" || n.SocksPassword == "" || n.Path != "" || n.SNI != "" || n.RealityDest != "" || n.RealityPrivateKey != "" || n.RealityPublicKey != "" {
				t.Fatal("existing socks defaults changed")
			}
		}},
		{"socks_explicit", `{"name":"Synthetic new","port":32101,"protocol":"socks","socks_username":"synthetic-user","socks_password":"synthetic-only","socks_udp":true}`, func(t *testing.T, n model.Node) {
			if n.SocksUsername != "synthetic-user" || n.SocksPassword != "synthetic-only" || !n.SocksUDP {
				t.Fatal("explicit socks parameters changed")
			}
		}},
		{"reality", `{"name":"Synthetic new","port":32101,"security":"reality","transport":"ws","path":"/old","sni":"synthetic.invalid","reality_dest":"synthetic.invalid:443"}`, func(t *testing.T, n model.Node) {
			pub, err := xray.PublicKeyFromPrivate(n.RealityPrivateKey)
			if err != nil || pub != n.RealityPublicKey || n.RealityShortID == "" || n.Fingerprint != "chrome" || n.RealitySpiderX != "/" || n.Transport != "tcp" || n.Path != "" || n.SNI != "synthetic.invalid" || n.RealityDest != "synthetic.invalid:443" {
				t.Fatal("existing Reality creation behavior changed")
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := nodeCreateAPIFixture(t)
			before := b2APISnapshot(s.Data)
			req := persistenceRequest(t, http.MethodPost, "/api/nodes", tc.payload, false)
			response := httptest.NewRecorder()
			NewRouter(s).ServeHTTP(response, req)
			if response.Code != http.StatusCreated {
				t.Fatalf("create status = %d", response.Code)
			}
			var node model.Node
			if err := json.Unmarshal(response.Body.Bytes(), &node); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(node.ID, "node_") || node.ID == "caller-id" || !node.Enabled || node.CreatedAt.IsZero() || !node.CreatedAt.Equal(node.UpdatedAt) || !node.CreatedAt.After(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
				t.Fatal("server-owned ID, enabled or timestamps changed")
			}
			if node.Host != before.Servers[node.ServerID].Host {
				t.Fatal("existing host selection changed")
			}
			tc.check(t, node)
			stored, exists := s.Data.Nodes[node.ID]
			if !exists {
				t.Fatal("201 without committed node")
			}
			a, b := b2CompareTimes(t, reflect.ValueOf(node), reflect.ValueOf(stored))
			if !reflect.DeepEqual(a.Interface(), b.Interface()) {
				t.Fatal("response node differs from full committed node")
			}
			if len(s.Data.Nodes) != len(before.Nodes)+1 || len(s.Data.OperationLogs) != len(before.OperationLogs)+1 {
				t.Fatal("create must add exactly one node and business log")
			}
			after := b2APISnapshot(s.Data)
			delete(after.Nodes, node.ID)
			for id, entry := range after.OperationLogs {
				if _, exists := before.OperationLogs[id]; exists {
					continue
				}
				if entry.ID != id || entry.Actor != "synthetic-admin" || entry.Action != "node.create" || entry.IP != clientIP(req) || entry.Detail != node.Name || entry.CreatedAt.IsZero() {
					t.Fatal("creation business log changed")
				}
				delete(after.OperationLogs, id)
			}
			a, b = b2CompareTimes(t, reflect.ValueOf(before), reflect.ValueOf(after))
			if !reflect.DeepEqual(a.Interface(), b.Interface()) {
				t.Fatal("create changed unrelated full memory, bindings, BBR or logs")
			}
			assertNodeFileMatchesMemory(t, s)
			info, err := os.Stat(s.Path)
			if err != nil {
				t.Fatal(err)
			}
			if info.ModTime().Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
				t.Fatal("successful create did not replace persisted file")
			}
		})
	}
}

func TestNodeCreateAPIInvalidParametersLeaveStateUnchanged(t *testing.T) {
	cases := []struct{ name, payload string }{
		{"invalid_json", `{`},
		{"wrong_type", `{"name":"Synthetic new","port":"invalid"}`},
		{"missing_name", `{"port":32101}`},
		{"missing_server", `{"name":"Synthetic new","server_id":"missing","port":32101}`},
		{"port_zero", `{"name":"Synthetic new","port":0}`},
		{"port_too_large", `{"name":"Synthetic new","port":65536}`},
		{"protocol", `{"name":"Synthetic new","port":32101,"protocol":"unsupported"}`},
		{"transport", `{"name":"Synthetic new","port":32101,"transport":"unsupported"}`},
		{"security", `{"name":"Synthetic new","port":32101,"security":"unsupported"}`},
		{"enabled_duplicate_port", `{"name":"Synthetic new","server_id":"srv_a","port":32001,"enabled":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := nodeCreateAPIFixture(t)
			raw, modified := persistDeletionFixture(t, s)
			before := b2APISnapshot(s.Data)
			response := b2APIRequest(t, s, http.MethodPost, "/api/nodes", tc.payload)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid create status = %d", response.Code)
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if message, ok := body["error"].(string); !ok || message == "" {
				t.Fatal("400 must contain an error")
			}
			if _, ok := body["id"]; ok {
				t.Fatal("invalid create returned success data")
			}
			b2APIUnchanged(t, s, before, s.Path, raw, modified)
		})
	}
}

func TestNodeCreateAPISaveFailureReturns500AndKeepsFullState(t *testing.T) {
	for _, mode := range []string{"serialization", "create", "rename"} {
		t.Run(mode, func(t *testing.T) {
			s := nodeCreateAPIFixture(t)
			path := s.Path
			raw, modified := persistDeletionFixture(t, s)
			var keep string
			var keepTime time.Time
			switch mode {
			case "serialization":
				server := s.Data.Servers["srv_b"]
				server.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
				s.Data.Servers[server.ID] = server
			case "create":
				s.Path = filepath.Join(filepath.Dir(path), "missing-parent", "panel.json")
			case "rename":
				s.Path = filepath.Join(filepath.Dir(path), "nonempty-target")
				if err := os.Mkdir(s.Path, 0700); err != nil {
					t.Fatal(err)
				}
				keep = filepath.Join(s.Path, "keep")
				if err := os.WriteFile(keep, raw, 0600); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(keep)
				if err != nil {
					t.Fatal(err)
				}
				keepTime = info.ModTime()
			}
			faultPath := s.Path
			before := b2APISnapshot(s.Data)
			response := b2APIRequest(t, s, http.MethodPost, "/api/nodes", `{"name":"Synthetic new","port":32101,"protocol":"socks","socks_username":"synthetic-user","socks_password":"synthetic-only"}`)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("save failure status = %d", response.Code)
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 1 || body["error"] != "Failed to save node mutation; no changes committed." {
				t.Fatal("save failure leaked details or returned success data")
			}
			if strings.Contains(response.Body.String(), faultPath) || strings.Contains(response.Body.String(), "synthetic-only") {
				t.Fatal("error leaked credentials or internal path")
			}
			b2APIUnchanged(t, s, before, path, raw, modified)
			if s.Path != faultPath {
				t.Fatal("create changed store path")
			}
			if keep != "" {
				b2APIUnchanged(t, s, before, keep, raw, keepTime)
			}
		})
	}
}
