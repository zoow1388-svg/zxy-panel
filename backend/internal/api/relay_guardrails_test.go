// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"zxy-panel/backend/internal/model"
	"zxy-panel/backend/internal/store"
)

func b2APIFixture(t *testing.T, referenced bool) *store.Store {
	t.Helper()
	s := deletionSafetyFixture(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.Data.Nodes["node"] = model.Node{ID: "node", ServerID: "srv_a", Name: "Synthetic socks", Protocol: "socks", Host: "192.0.2.20", Port: 32001, SocksUsername: "synthetic-user", SocksPassword: "synthetic-only", Enabled: true, CreatedAt: now, UpdatedAt: now}
	n := s.Data.Nodes["node"]
	n.ID = "node_b"
	n.ServerID = "srv_b"
	n.Port = 32002
	s.Data.Nodes[n.ID] = n
	n.ID = "tcp_node"
	n.Protocol = "vless"
	n.Security = "reality"
	n.Transport = "tcp"
	n.Port = 32003
	s.Data.Nodes[n.ID] = n
	s.Data.RelayRoutes["relay"] = model.RelayRoute{ID: "relay", Name: "Synthetic relay", RelayServerID: "srv_b", RelayHost: "192.0.2.30", RelayPort: 33001, RouteMode: "socks5_route", LandingMode: "manual_socks5", ManualSocksHost: "192.0.2.40", ManualSocksPort: 1080, ManualSocksUsername: "synthetic-user", ManualSocksPassword: "synthetic-only", RelayNetwork: "tcp", RelayRealityPrivateKey: "synthetic-private", RelayRealityPublicKey: "synthetic-public", RelayRealityShortID: "synthetic-short", Enabled: true, CreatedAt: now, UpdatedAt: now}
	c := model.Client{ID: "client", Username: "Synthetic client", UUID: "synthetic-uuid", SubscribeToken: "synthetic-token", NodeIDs: []string{}, RelayRouteIDs: []string{}, Enabled: true, CreatedAt: now, UpdatedAt: now}
	if referenced {
		c.RelayRouteIDs = []string{"relay"}
	}
	s.Data.Clients[c.ID] = c
	s.Data.LandingExits["exit"] = model.LandingExit{ID: "exit", Host: "192.0.2.40", Port: 1080, Username: "synthetic-user", Password: "synthetic-only", UDP: true, Enabled: true}
	server := s.Data.Servers["srv_a"]
	server.BBRPendingAction = &model.AgentSystemAction{ID: "synthetic-pending", Action: "bbr-status"}
	server.BBRStatus.AvailableCongestionControl = []string{"cubic", "bbr"}
	s.Data.Servers["srv_a"] = server
	persistDeletionFixture(t, s)
	return s
}

func b2APISnapshot(data model.PanelData) model.PanelData {
	data.Admins = maps.Clone(data.Admins)
	data.Servers = maps.Clone(data.Servers)
	data.Nodes = maps.Clone(data.Nodes)
	data.Clients = maps.Clone(data.Clients)
	data.RelayRoutes = maps.Clone(data.RelayRoutes)
	data.LandingExits = maps.Clone(data.LandingExits)
	data.OperationLogs = maps.Clone(data.OperationLogs)
	for id, server := range data.Servers {
		server.BBRStatus.AvailableCongestionControl = slices.Clone(server.BBRStatus.AvailableCongestionControl)
		if server.BBRPendingAction != nil {
			action := *server.BBRPendingAction
			server.BBRPendingAction = &action
		}
		data.Servers[id] = server
	}
	for id, client := range data.Clients {
		client.NodeIDs = slices.Clone(client.NodeIDs)
		client.RelayRouteIDs = slices.Clone(client.RelayRouteIDs)
		data.Clients[id] = client
	}
	data.NetworkPolicy.DNSServers = slices.Clone(data.NetworkPolicy.DNSServers)
	data.NetworkPolicyBackup.DNSServers = slices.Clone(data.NetworkPolicyBackup.DNSServers)
	return data
}

func b2APIUnchanged(t *testing.T, s *store.Store, before model.PanelData, path string, raw []byte, modified time.Time) {
	t.Helper()
	want, got := b2CompareTimes(t, reflect.ValueOf(before), reflect.ValueOf(s.Data))
	if !reflect.DeepEqual(want.Interface(), got.Interface()) {
		t.Fatal("official memory, bindings, BBR or logs changed")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, after) || !info.ModTime().Equal(modified) {
		t.Fatal("database bytes or modification time changed")
	}
}

// Compare every time by instant, then DeepEqual all remaining fields in isolated copies.
func b2CompareTimes(t *testing.T, a, b reflect.Value) (reflect.Value, reflect.Value) {
	t.Helper()
	if a.Type() == reflect.TypeOf(time.Time{}) {
		if !a.Interface().(time.Time).Equal(b.Interface().(time.Time)) {
			t.Fatal("memory timestamp changed")
		}
		return reflect.Zero(a.Type()), reflect.Zero(b.Type())
	}
	switch a.Kind() {
	case reflect.Struct:
		x, y := reflect.New(a.Type()).Elem(), reflect.New(b.Type()).Elem()
		x.Set(a)
		y.Set(b)
		for i := 0; i < a.NumField(); i++ {
			u, v := b2CompareTimes(t, a.Field(i), b.Field(i))
			x.Field(i).Set(u)
			y.Field(i).Set(v)
		}
		return x, y
	case reflect.Map:
		if a.IsNil() || b.IsNil() || a.Len() != b.Len() {
			return a, b
		}
		x, y := reflect.MakeMapWithSize(a.Type(), a.Len()), reflect.MakeMapWithSize(b.Type(), b.Len())
		for _, key := range a.MapKeys() {
			bv := b.MapIndex(key)
			if !bv.IsValid() {
				return a, b
			}
			u, v := b2CompareTimes(t, a.MapIndex(key), bv)
			x.SetMapIndex(key, u)
			y.SetMapIndex(key, v)
		}
		return x, y
	case reflect.Slice:
		if a.IsNil() || b.IsNil() || a.Len() != b.Len() {
			return a, b
		}
		x, y := reflect.MakeSlice(a.Type(), a.Len(), a.Len()), reflect.MakeSlice(b.Type(), b.Len(), b.Len())
		for i := 0; i < a.Len(); i++ {
			u, v := b2CompareTimes(t, a.Index(i), b.Index(i))
			x.Index(i).Set(u)
			y.Index(i).Set(v)
		}
		return x, y
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			return a, b
		}
		u, v := b2CompareTimes(t, a.Elem(), b.Elem())
		x, y := reflect.New(a.Type().Elem()), reflect.New(b.Type().Elem())
		x.Elem().Set(u)
		y.Elem().Set(v)
		return x, y
	default:
		return a, b
	}
}

func b2RelayPayload(t *testing.T, relay model.RelayRoute, changes map[string]any, omit ...string) string {
	t.Helper()
	raw, err := json.Marshal(relay)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range omit {
		delete(fields, key)
	}
	for key, value := range changes {
		fields[key] = value
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func b2APIRequest(t *testing.T, s *store.Store, method, path, payload string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	NewRouter(s).ServeHTTP(response, persistenceRequest(t, method, path, payload, false))
	return response
}

func b2APIFault(t *testing.T, s *store.Store, mode string) {
	t.Helper()
	switch mode {
	case "serialization":
		v := s.Data.Servers["srv_a"]
		v.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
		s.Data.Servers["srv_a"] = v
	case "create":
		s.Path = filepath.Join(filepath.Dir(s.Path), "missing-parent", "panel.json")
	case "rename":
		s.Path = filepath.Join(filepath.Dir(s.Path), "nonempty-target")
		if err := os.Mkdir(s.Path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(s.Path, "keep"), []byte("synthetic-evidence"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestB2RelayAPIDeleteReferences(t *testing.T) {
	for _, status := range []string{"ordinary", "disabled", "expired", "unreferenced"} {
		t.Run(status, func(t *testing.T) {
			s := b2APIFixture(t, status != "unreferenced")
			c := s.Data.Clients["client"]
			c.Enabled = status != "disabled"
			if status == "expired" {
				c.ExpireAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			s.Data.Clients[c.ID] = c
			raw, modified := persistDeletionFixture(t, s)
			before := b2APISnapshot(s.Data)
			response := b2APIRequest(t, s, "DELETE", "/api/relays/relay", "")
			if status == "unreferenced" {
				if response.Code != 200 || len(s.Data.RelayRoutes) != 0 || !reflect.DeepEqual(before.Clients, s.Data.Clients) {
					t.Fatal("unreferenced relay deletion contract changed")
				}
				return
			}
			if response.Code != 409 {
				t.Fatalf("expected 409, got %d: %s", response.Code, response.Body.String())
			}
			b2APIUnchanged(t, s, before, s.Path, raw, modified)
		})
	}
}

func TestB2RelayAPIPUTProtectedFields(t *testing.T) {
	changes := map[string]map[string]any{
		"server": {"relay_server_id": "srv_a"}, "route": {"route_mode": "tcp_forward", "landing_node_id": "tcp_node"}, "landing": {"landing_mode": "panel_node", "landing_node_id": "node"},
		"host": {"manual_socks_host": "192.0.2.41"}, "port": {"manual_socks_port": 1081}, "username": {"manual_socks_username": "synthetic-other"}, "password": {"manual_socks_password": "synthetic-other"}, "udp": {"manual_socks_udp": true},
		"empty_server": {"relay_server_id": ""}, "null_server": {"relay_server_id": nil}, "wrong_server": {"relay_server_id": 7}, "null_mode": {"route_mode": nil}, "wrong_mode": {"route_mode": 7}, "null_node": {"landing_node_id": nil}, "wrong_udp": {"manual_socks_udp": "true"},
		"ordinary": {"name": "After", "remark": "Synthetic note"},
	}
	for name, fields := range changes {
		t.Run(name, func(t *testing.T) {
			s := b2APIFixture(t, true)
			raw, modified := persistDeletionFixture(t, s)
			before := b2APISnapshot(s.Data)
			response := b2APIRequest(t, s, "PUT", "/api/relays/relay", b2RelayPayload(t, s.Data.RelayRoutes["relay"], fields))
			want := 409
			if strings.HasPrefix(name, "empty_") || strings.HasPrefix(name, "null_") || strings.HasPrefix(name, "wrong_") {
				want = 400
			}
			if name == "ordinary" {
				want = 200
			}
			if response.Code != want {
				t.Fatalf("expected %d, got %d: %s", want, response.Code, response.Body.String())
			}
			if want != 200 {
				b2APIUnchanged(t, s, before, s.Path, raw, modified)
			} else if s.Data.RelayRoutes["relay"].Name != "After" || !reflect.DeepEqual(before.Clients, s.Data.Clients) {
				t.Fatal("ordinary update changed bindings")
			}
		})
	}
	for _, mode := range []string{"omitted", "tcp_legacy", "socks_legacy", "panel_node", "stale_manual_node"} {
		t.Run(mode, func(t *testing.T) {
			s := b2APIFixture(t, true)
			relay := s.Data.RelayRoutes["relay"]
			if mode == "tcp_legacy" {
				relay.RouteMode = ""
				relay.LandingNodeID = "tcp_node"
			}
			if mode == "socks_legacy" {
				relay.LandingMode = ""
			}
			if mode == "panel_node" {
				relay.LandingMode = "panel_node"
				relay.LandingNodeID = "node"
			}
			if mode == "stale_manual_node" {
				relay.LandingNodeID = "node"
			}
			s.Data.RelayRoutes["relay"] = relay
			raw, modified := persistDeletionFixture(t, s)
			before := b2APISnapshot(s.Data)
			payload := b2RelayPayload(t, relay, map[string]any{"name": "After"}, "relay_server_id", "route_mode", "landing_mode", "landing_node_id", "manual_socks_host", "manual_socks_port", "manual_socks_username", "manual_socks_password", "manual_socks_udp")
			response := b2APIRequest(t, s, "PUT", "/api/relays/relay", payload)
			if mode == "stale_manual_node" {
				if response.Code != 409 {
					t.Fatal("implicit landing reference removal must conflict")
				}
				b2APIUnchanged(t, s, before, s.Path, raw, modified)
				return
			}
			if response.Code != 200 {
				t.Fatalf("same effective ownership rejected: %d %s", response.Code, response.Body.String())
			}
			actual := s.Data.RelayRoutes["relay"]
			if actual.RelayServerID != relay.RelayServerID || actual.ManualSocksPassword != relay.ManualSocksPassword || actual.ManualSocksUDP != relay.ManualSocksUDP || actual.LandingNodeID != relay.LandingNodeID || !actual.CreatedAt.Equal(relay.CreatedAt) {
				t.Fatal("omitted protected fields changed")
			}
		})
	}
}

func TestB2RelayAPIParentsAndIdentity(t *testing.T) {
	for _, mode := range []string{"relay_id", "server_id", "missing_server", "node_id", "node_server", "missing_node", "missing_object"} {
		t.Run(mode, func(t *testing.T) {
			s := b2APIFixture(t, false)
			relay := s.Data.RelayRoutes["relay"]
			relay.LandingMode = "panel_node"
			relay.LandingNodeID = "node"
			s.Data.RelayRoutes["relay"] = relay
			switch mode {
			case "relay_id":
				v := relay
				v.ID = "other"
				s.Data.RelayRoutes["relay"] = v
			case "server_id":
				v := s.Data.Servers["srv_b"]
				v.ID = "other"
				s.Data.Servers["srv_b"] = v
			case "missing_server":
				delete(s.Data.Servers, "srv_b")
			case "node_id":
				v := s.Data.Nodes["node"]
				v.ID = "other"
				s.Data.Nodes["node"] = v
			case "node_server":
				v := s.Data.Nodes["node"]
				v.ServerID = "missing"
				s.Data.Nodes["node"] = v
			case "missing_node":
				delete(s.Data.Nodes, "node")
			}
			raw, modified := persistDeletionFixture(t, s)
			before := b2APISnapshot(s.Data)
			path := "/api/relays/relay"
			if mode == "missing_object" {
				path = "/api/relays/missing"
			}
			response := b2APIRequest(t, s, "PUT", path, b2RelayPayload(t, relay, nil))
			want := 409
			if mode == "missing_node" {
				want = 400
			}
			if mode == "missing_object" {
				want = 404
			}
			if response.Code != want {
				t.Fatalf("expected %d, got %d: %s", want, response.Code, response.Body.String())
			}
			b2APIUnchanged(t, s, before, s.Path, raw, modified)
		})
	}
}

func TestB2RelayAPIUnsupportedLandingModeIsBadRequest(t *testing.T) {
	for _, method := range []string{"POST", "PUT"} {
		for _, mode := range []string{"manual_socks5", "unsupported"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				s := b2APIFixture(t, true)
				relay := s.Data.RelayRoutes["relay"]
				path := "/api/relays/relay"
				if method == "POST" {
					path = "/api/relays"
					relay.RelayPort++
				}
				raw, modified := persistDeletionFixture(t, s)
				before := b2APISnapshot(s.Data)
				payload := b2RelayPayload(t, relay, map[string]any{"landing_mode": mode})
				response := b2APIRequest(t, s, method, path, payload)
				want := 200
				if method == "POST" {
					want = 201
				}
				if mode == "unsupported" {
					want = 400
				}
				if response.Code != want {
					t.Fatalf("expected %d, got %d: %s", want, response.Code, response.Body.String())
				}
				if mode == "unsupported" {
					b2APIUnchanged(t, s, before, s.Path, raw, modified)
				}
			})
		}
	}
}

func TestB2RelayAPIPersistenceFailures(t *testing.T) {
	for _, operation := range []string{"POST", "PUT", "DELETE"} {
		for _, mode := range []string{"serialization", "create", "rename"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				s := b2APIFixture(t, false)
				path := s.Path
				raw, modified := persistDeletionFixture(t, s)
				b2APIFault(t, s, mode)
				before := b2APISnapshot(s.Data)
				url := "/api/relays/relay"
				relay := s.Data.RelayRoutes["relay"]
				if operation == "POST" {
					url = "/api/relays"
					relay.RelayPort++
				}
				response := b2APIRequest(t, s, operation, url, b2RelayPayload(t, relay, nil))
				if response.Code != 500 {
					t.Fatalf("save failure returned %d: %s", response.Code, response.Body.String())
				}
				if strings.Contains(response.Body.String(), filepath.Dir(path)) || strings.Contains(response.Body.String(), "synthetic-only") || strings.Contains(response.Body.String(), "synthetic-private") {
					t.Fatal("error leaked path or credentials")
				}
				b2APIUnchanged(t, s, before, path, raw, modified)
			})
		}
	}
}

func TestB2RelayAPICreateDefaultAndReadOnly(t *testing.T) {
	s := b2APIFixture(t, false)
	relay := s.Data.RelayRoutes["relay"]
	relay.RelayPort++
	relay.RelayServerID = ""
	response := b2APIRequest(t, s, "POST", "/api/relays", b2RelayPayload(t, relay, nil))
	if response.Code != 201 {
		t.Fatalf("default relay create failed: %d %s", response.Code, response.Body.String())
	}
	var created model.RelayRoute
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.RelayServerID == "" || !created.Enabled || len(s.Data.RelayRoutes) != 2 || len(s.Data.OperationLogs) != 1 {
		t.Fatal("relay create contract changed")
	}
	raw, modified := persistDeletionFixture(t, s)
	before := b2APISnapshot(s.Data)
	for _, path := range []string{"/api/relays", "/api/relays/relay"} {
		if result := b2APIRequest(t, s, "GET", path, ""); result.Code != 200 {
			t.Fatal("read failed")
		}
	}
	b2APIUnchanged(t, s, before, s.Path, raw, modified)
}
