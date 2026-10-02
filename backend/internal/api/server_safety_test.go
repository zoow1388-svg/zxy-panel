// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"zxy-panel/backend/internal/model"
	"zxy-panel/backend/internal/security"
	"zxy-panel/backend/internal/store"
)

func readSafetyFixture(t *testing.T, serverCount int, sameVersion bool) (*store.Store, []byte) {
	t.Helper()
	t.Setenv("ZXY_LOCAL_SERVER_IP", "192.0.2.10")
	t.Setenv("ZXY_LOCAL_SERVER_HOST", "192.0.2.10")
	t.Setenv("ZXY_JWT_SECRET", "synthetic-safety-secret-not-production")
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	data := model.PanelData{
		Version: "0.7.8-stable-engineering",
		Admins:  map[string]model.AdminUser{}, Servers: map[string]model.Server{},
		Nodes: map[string]model.Node{}, Clients: map[string]model.Client{},
		RelayRoutes: map[string]model.RelayRoute{}, LandingExits: map[string]model.LandingExit{},
		OperationLogs: map[string]model.OperationLog{},
	}
	for i := 0; i < serverCount; i++ {
		id := "srv_a"
		version := "0.7.8-stable-engineering"
		status := "online"
		if i == 1 {
			id, status = "srv_b", "offline"
			if !sameVersion {
				version = "0.7.7.6-bbr-optimization-agent-xray"
			}
		}
		data.Servers[id] = model.Server{
			ID: id, Name: "Synthetic " + id, IP: "192.0.2.10", Host: "192.0.2.10",
			Region: "TEST", Provider: "Synthetic-only", Status: status,
			AgentToken: "synthetic-token-" + id, AgentVersion: version,
			LastSyncAt: now, CreatedAt: now, UpdatedAt: now,
			BBRPendingAction: &model.AgentSystemAction{ID: "action_" + id, Action: "bbr-status"},
		}
		nodeID, relayID := "node_"+id, "relay_"+id
		data.Nodes[nodeID] = model.Node{ID: nodeID, ServerID: id, Port: 31001 + i, Enabled: true}
		data.RelayRoutes[relayID] = model.RelayRoute{ID: relayID, RelayServerID: id, LandingNodeID: nodeID, Enabled: true}
		data.Clients["client_"+id] = model.Client{ID: "client_" + id, NodeIDs: []string{nodeID}, RelayRouteIDs: []string{relayID}, Enabled: true}
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "synthetic-panel.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	return &store.Store{Path: path, Data: data}, raw
}

func authenticatedSafetyRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	token, err := security.SignJWT("synthetic-safety-secret-not-production", security.Claims{
		Sub: "synthetic-admin", Username: "synthetic-admin", Role: "super_admin",
		Exp: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func TestReadEndpointsDoNotMergeMigrateOrWrite(t *testing.T) {
	paths := []string{"/api/servers", "/api/nodes", "/api/system/optimization/bbr/status"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			for _, sameVersion := range []bool{false, true} {
				for _, serverCount := range []int{0, 2} {
					s, beforeFile := readSafetyFixture(t, serverCount, sameVersion)
					beforeState, err := json.Marshal(s.Data)
					if err != nil {
						t.Fatal(err)
					}
					info, err := os.Stat(s.Path)
					if err != nil {
						t.Fatal(err)
					}
					handler := NewRouter(s)
					for refresh := 0; refresh < 3; refresh++ {
						response := httptest.NewRecorder()
						handler.ServeHTTP(response, authenticatedSafetyRequest(t, http.MethodGet, path))
						if response.Code != http.StatusOK {
							t.Fatalf("GET %s returned HTTP %d", path, response.Code)
						}
						afterState, err := json.Marshal(s.Data)
						if err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(beforeState, afterState) {
							t.Errorf("GET %s refresh %d mutated identity, references or BBR state", path, refresh)
						}
						afterFile, err := os.ReadFile(s.Path)
						if err != nil {
							t.Fatal(err)
						}
						if !bytes.Equal(beforeFile, afterFile) {
							t.Errorf("GET %s refresh %d wrote the database", path, refresh)
						}
						afterInfo, err := os.Stat(s.Path)
						if err != nil {
							t.Fatal(err)
						}
						if !afterInfo.ModTime().Equal(info.ModTime()) {
							t.Errorf("GET %s refresh %d rewrote the database file", path, refresh)
						}
						if len(s.Data.Servers) != serverCount {
							t.Errorf("GET %s changed server count: expected %d got %d", path, serverCount, len(s.Data.Servers))
						}
						if serverCount == 2 {
							router := &Router{store: s}
							for _, id := range []string{"srv_a", "srv_b"} {
								request := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", nil)
								request.Header.Set("X-Agent-Token", "synthetic-token-"+id)
								if !router.validateAgentToken(httptest.NewRecorder(), request, id) {
									t.Errorf("GET %s invalidated original Agent identity %s", path, id)
								}
							}
						}
					}
				}
			}
		})
	}
}

func TestRejectedCreationDoesNotMergeOrWrite(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
	}{
		{"node_missing_name", "/api/nodes", `{"port":33001}`},
		{"relay_missing_port", "/api/relays", `{"name":"Synthetic relay","route_mode":"tcp_forward"}`},
		{"client_relay_missing_port", "/api/clients/create-socks5-relay", `{"username":"Synthetic client","landing_exit_id":"exit_synthetic"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, beforeFile := readSafetyFixture(t, 2, true)
			s.Data.LandingExits["exit_synthetic"] = model.LandingExit{
				ID: "exit_synthetic", Host: "exit.invalid", Port: 1080, Enabled: true,
			}
			beforeState, err := json.Marshal(s.Data)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(s.Path)
			if err != nil {
				t.Fatal(err)
			}
			request := authenticatedSafetyRequest(t, http.MethodPost, tc.path)
			request.Header.Set("Content-Type", "application/json")
			request.Body = io.NopCloser(bytes.NewBufferString(tc.body))
			response := httptest.NewRecorder()
			NewRouter(s).ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid creation returned HTTP %d, expected 400", response.Code)
			}
			afterState, err := json.Marshal(s.Data)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(beforeState, afterState) {
				t.Error("rejected creation mutated server identities, references or pending actions")
			}
			afterFile, err := os.ReadFile(s.Path)
			if err != nil {
				t.Fatal(err)
			}
			afterInfo, err := os.Stat(s.Path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(beforeFile, afterFile) || !afterInfo.ModTime().Equal(info.ModTime()) {
				t.Error("rejected creation wrote the database")
			}
		})
	}
}

func deletionSafetyFixture(t *testing.T) *store.Store {
	t.Helper()
	s, _ := readSafetyFixture(t, 2, true)
	t.Setenv("ZXY_LOCAL_SERVER_ID", "srv_a")
	s.Data.Nodes = map[string]model.Node{}
	s.Data.RelayRoutes = map[string]model.RelayRoute{}
	s.Data.Clients = map[string]model.Client{}
	for id, server := range s.Data.Servers {
		server.BBRPendingAction = nil
		s.Data.Servers[id] = server
	}
	return s
}

func persistDeletionFixture(t *testing.T, s *store.Store) ([]byte, time.Time) {
	t.Helper()
	raw, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	modified := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(s.Path, modified, modified); err != nil {
		t.Fatal(err)
	}
	return raw, modified
}

func assertDeletionUnchanged(t *testing.T, s *store.Store, before []byte, modified time.Time) {
	t.Helper()
	state, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, state) {
		t.Error("rejected deletion changed servers, references, pending actions or operation logs")
	}
	file, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, file) || !info.ModTime().Equal(modified) {
		t.Error("rejected deletion wrote the database")
	}
	if _, err := os.Stat(s.Path + ".tmp"); !os.IsNotExist(err) {
		t.Error("rejected deletion created a temporary database file")
	}
}

func TestServerDeleteConflictsPreserveData(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T, *store.Store)
		want  []string
	}{
		{"Case_C_relay_reference", func(t *testing.T, s *store.Store) {
			s.Data.RelayRoutes["relay_b"] = model.RelayRoute{ID: "relay_b", RelayServerID: "srv_b", Enabled: true}
		}, []string{"relay_reference:relay_b"}},
		{"disabled_relay", func(t *testing.T, s *store.Store) {
			s.Data.RelayRoutes["relay_b"] = model.RelayRoute{ID: "relay_b", RelayServerID: "srv_b", Enabled: false}
		}, []string{"relay_reference:relay_b"}},
		{"active_node", func(t *testing.T, s *store.Store) {
			s.Data.Nodes["node_b"] = model.Node{ID: "node_b", ServerID: "srv_b", Enabled: true}
		}, []string{"node_reference:node_b"}},
		{"disabled_node", func(t *testing.T, s *store.Store) {
			s.Data.Nodes["node_b"] = model.Node{ID: "node_b", ServerID: "srv_b", Enabled: false}
		}, []string{"node_reference:node_b"}},
		{"last_server", func(t *testing.T, s *store.Store) {
			delete(s.Data.Servers, "srv_a")
			t.Setenv("ZXY_LOCAL_SERVER_ID", "srv_b")
		}, []string{"last_server_protected:srv_b", "local_server_protected:srv_b"}},
		{"pending_bbr_action", func(t *testing.T, s *store.Store) {
			server := s.Data.Servers["srv_b"]
			server.BBRPendingAction = &model.AgentSystemAction{ID: "action_b", Action: "enable-bbr"}
			s.Data.Servers["srv_b"] = server
		}, []string{"bbr_pending_action:action_b"}},
		{"empty_pending_bbr_action", func(t *testing.T, s *store.Store) {
			server := s.Data.Servers["srv_b"]
			server.BBRPendingAction = &model.AgentSystemAction{}
			s.Data.Servers["srv_b"] = server
		}, []string{"bbr_pending_action:"}},
		{"local_server", func(t *testing.T, s *store.Store) {
			t.Setenv("ZXY_LOCAL_SERVER_ID", "srv_b")
		}, []string{"local_server_protected:srv_b"}},
		{"unknown_local_identity", func(t *testing.T, s *store.Store) {
			t.Setenv("ZXY_LOCAL_SERVER_ID", "")
		}, []string{"local_identity_unknown:"}},
		{"stale_local_identity", func(t *testing.T, s *store.Store) {
			t.Setenv("ZXY_LOCAL_SERVER_ID", "srv_missing")
		}, []string{"local_identity_invalid:"}},
		{"inconsistent_local_record", func(t *testing.T, s *store.Store) {
			server := s.Data.Servers["srv_a"]
			server.ID = "srv_other"
			s.Data.Servers["srv_a"] = server
		}, []string{"local_identity_invalid:"}},
		{"disabled_client_node_reference", func(t *testing.T, s *store.Store) {
			s.Data.Nodes["node_b"] = model.Node{ID: "node_b", ServerID: "srv_b", Enabled: false}
			s.Data.Clients["client_b"] = model.Client{ID: "client_b", NodeIDs: []string{"node_b"}, Enabled: false}
		}, []string{"node_reference:node_b", "client_reference:client_b"}},
		{"disabled_client_relay_reference", func(t *testing.T, s *store.Store) {
			s.Data.RelayRoutes["relay_b"] = model.RelayRoute{ID: "relay_b", RelayServerID: "srv_b", Enabled: false}
			s.Data.Clients["client_b"] = model.Client{ID: "client_b", RelayRouteIDs: []string{"relay_b"}, Enabled: false}
		}, []string{"relay_reference:relay_b", "client_reference:client_b"}},
		{"relay_landing_and_client_reference", func(t *testing.T, s *store.Store) {
			s.Data.Nodes["node_b"] = model.Node{ID: "node_b", ServerID: "srv_b", Enabled: false}
			s.Data.RelayRoutes["relay_a"] = model.RelayRoute{ID: "relay_a", RelayServerID: "srv_a", LandingNodeID: "node_b", Enabled: false}
			s.Data.Clients["client_b"] = model.Client{ID: "client_b", RelayRouteIDs: []string{"relay_a"}, Enabled: false}
		}, []string{"node_reference:node_b", "relay_reference:relay_a", "client_reference:client_b"}},
		{"all_conflicts_sorted_and_deduplicated", func(t *testing.T, s *store.Store) {
			s.Data.Nodes["node_b"] = model.Node{ID: "node_b", ServerID: "srv_b"}
			s.Data.Nodes["node_c"] = model.Node{ID: "node_c", ServerID: "srv_b"}
			s.Data.RelayRoutes["relay_b"] = model.RelayRoute{ID: "relay_b", RelayServerID: "srv_b", LandingNodeID: "node_b"}
			s.Data.Clients["client_b"] = model.Client{ID: "client_b", NodeIDs: []string{"node_b", "node_c"}, RelayRouteIDs: []string{"relay_b", "relay_b"}}
			server := s.Data.Servers["srv_b"]
			server.BBRPendingAction = &model.AgentSystemAction{ID: "action_b"}
			s.Data.Servers["srv_b"] = server
		}, []string{"node_reference:node_b", "node_reference:node_c", "relay_reference:relay_b", "client_reference:client_b", "bbr_pending_action:action_b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := deletionSafetyFixture(t)
			tc.setup(t, s)
			before, modified := persistDeletionFixture(t, s)
			handler := NewRouter(s)
			for attempt := 0; attempt < 3; attempt++ {
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, authenticatedSafetyRequest(t, http.MethodDelete, "/api/servers/srv_b"))
				if response.Code != http.StatusConflict {
					t.Errorf("delete returned HTTP %d, expected conflict 409", response.Code)
				}
				var result struct {
					Error     string `json:"error"`
					Code      string `json:"code"`
					Conflicts []struct {
						Kind       string `json:"kind"`
						ResourceID string `json:"resource_id"`
					} `json:"conflicts"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Error == "" || result.Code != "server_delete_conflict" {
					t.Error("rejection must include a clear error and server_delete_conflict code")
				}
				got := make([]string, 0, len(result.Conflicts))
				for _, conflict := range result.Conflicts {
					got = append(got, conflict.Kind+":"+conflict.ResourceID)
				}
				want := append([]string(nil), tc.want...)
				sort.Strings(want)
				if !sort.StringsAreSorted(got) || strings.Join(got, "|") != strings.Join(want, "|") {
					t.Errorf("conflicts mismatch: got %v, want %v", got, want)
				}
				if bytes.Contains(response.Body.Bytes(), []byte("synthetic-token-")) {
					t.Error("deletion conflict leaked an Agent token")
				}
				assertDeletionUnchanged(t, s, before, modified)
			}
		})
	}
}

func TestServerDeleteUnreferencedRemotePreservesOtherData(t *testing.T) {
	s := deletionSafetyFixture(t)
	t.Setenv("ZXY_LOCAL_SERVER_ID", "  srv_a  ")
	s.Data.Nodes["node_orphan"] = model.Node{ID: "node_orphan", ServerID: "srv_missing"}
	s.Data.RelayRoutes["relay_orphan"] = model.RelayRoute{ID: "relay_orphan", RelayServerID: "srv_missing"}
	s.Data.Clients["client_orphan"] = model.Client{ID: "client_orphan", NodeIDs: []string{"node_missing"}, RelayRouteIDs: []string{"relay_missing"}}
	references := func() []byte {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"nodes": s.Data.Nodes, "relays": s.Data.RelayRoutes, "clients": s.Data.Clients, "exits": s.Data.LandingExits})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	beforeReferences := references()
	beforeLocal, err := json.Marshal(s.Data.Servers["srv_a"])
	if err != nil {
		t.Fatal(err)
	}
	persistDeletionFixture(t, s)
	handler := NewRouter(s)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedSafetyRequest(t, http.MethodDelete, "/api/servers/srv_b"))
	if response.Code != http.StatusOK {
		t.Fatalf("unreferenced remote deletion returned HTTP %d: %s", response.Code, response.Body)
	}
	var result map[string]bool
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || !result["ok"] {
		t.Fatal("successful deletion must retain the existing ok response")
	}
	if _, exists := s.Data.Servers["srv_b"]; exists || len(s.Data.Servers) != 1 {
		t.Error("only the selected remote server should be removed")
	}
	afterLocal, err := json.Marshal(s.Data.Servers["srv_a"])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeLocal, afterLocal) || !bytes.Equal(beforeReferences, references()) {
		t.Error("deleting a remote server changed local identity or cleaned unrelated orphan references")
	}
	if len(s.Data.OperationLogs) != 1 {
		t.Error("successful deletion should produce one operation log")
	}
	state, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.ReadFile(s.Path)
	if err != nil || !bytes.Equal(state, file) {
		t.Fatal("successful deletion was not persisted")
	}
	info, err := os.Stat(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedSafetyRequest(t, http.MethodDelete, "/api/servers/srv_b"))
	if response.Code != http.StatusNotFound {
		t.Error("repeated deletion of a missing ID must return 404")
	}
	assertDeletionUnchanged(t, s, state, info.ModTime())
}
