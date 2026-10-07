// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"zxy-panel/backend/internal/model"
	"zxy-panel/backend/internal/store"
)

func nodeGuardrailFixture(t *testing.T) *store.Store {
	t.Helper()
	s := deletionSafetyFixture(t)
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	s.Data.Nodes["node"] = model.Node{
		ID: "node", ServerID: "srv_a", Name: "Synthetic node", Host: "192.0.2.20",
		Port: 32001, Protocol: "vless", Transport: "tcp", Security: "none",
		Enabled: true, CreatedAt: now, UpdatedAt: now,
	}
	server := s.Data.Servers["srv_a"]
	server.BBRPendingAction = &model.AgentSystemAction{ID: "synthetic-pending", Action: "bbr-status"}
	s.Data.Servers["srv_a"] = server
	s.Mu.Lock()
	err := s.SaveLocked()
	s.Mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func nodeUpdatePayload(t *testing.T, node model.Node, serverField string) string {
	t.Helper()
	raw, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "server_id")
	if serverField != "" {
		fields["server_id"] = json.RawMessage(serverField)
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func addNodeGuardrailReference(s *store.Store, kind string) {
	if kind == "client" {
		s.Data.Clients["client"] = model.Client{ID: "client", Username: "Synthetic client", NodeIDs: []string{"node"}, Enabled: true}
	}
	if kind == "relay" {
		s.Data.RelayRoutes["relay"] = model.RelayRoute{ID: "relay", RelayServerID: "srv_b", LandingNodeID: "node", Enabled: true}
	}
}

func assertNodeConflictResponse(t *testing.T, response *httptest.ResponseRecorder, count int) {
	t.Helper()
	if response.Code != http.StatusConflict {
		t.Fatalf("expected HTTP 409, got %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		Error     string               `json:"error"`
		Conflicts []store.NodeConflict `json:"conflicts"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Error == "" || len(result.Conflicts) != count {
		t.Fatalf("missing error or reference details: %s", response.Body.String())
	}
	for _, conflict := range result.Conflicts {
		if conflict.Kind == "" || conflict.ResourceID == "" || conflict.Message == "" {
			t.Fatal("conflict must identify the blocking reference")
		}
	}
}

func assertNodeFileMatchesMemory(t *testing.T, s *store.Store) {
	t.Helper()
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil || !bytes.Equal(raw, want) {
		t.Fatal("committed disk and official memory differ")
	}
}

func TestNodeDeleteReferencesReturn409WithoutUnbinding(t *testing.T) {
	for _, reference := range []string{"client", "disabled_client", "expired_client", "relay", "disabled_relay", "manual_relay", "both", "disabled_node", "identity"} {
		t.Run(reference, func(t *testing.T) {
			s := nodeGuardrailFixture(t)
			switch reference {
			case "client", "disabled_client", "expired_client", "both", "disabled_node":
				addNodeGuardrailReference(s, "client")
				client := s.Data.Clients["client"]
				client.NodeIDs = []string{"node", "node"}
				client.Enabled = reference != "disabled_client"
				if reference == "expired_client" {
					client.ExpireAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
				}
				s.Data.Clients["client"] = client
			}
			if reference == "relay" || reference == "disabled_relay" || reference == "manual_relay" || reference == "both" {
				addNodeGuardrailReference(s, "relay")
				relay := s.Data.RelayRoutes["relay"]
				relay.Enabled = reference != "disabled_relay"
				if reference == "manual_relay" {
					relay.RouteMode, relay.LandingMode = "socks5_route", "manual_socks5"
				}
				s.Data.RelayRoutes["relay"] = relay
			}
			if reference == "disabled_node" || reference == "identity" {
				node := s.Data.Nodes["node"]
				if reference == "identity" {
					node.ID = "other"
				} else {
					node.Enabled = false
				}
				s.Data.Nodes["node"] = node
			}
			before := persistenceSnapshot(t, s.Data)
			file, modified := persistDeletionFixture(t, s)
			response := httptest.NewRecorder()
			NewRouter(s).ServeHTTP(response, authenticatedSafetyRequest(t, "DELETE", "/api/nodes/node"))
			count := 1
			if reference == "both" {
				count = 2
			}
			assertNodeConflictResponse(t, response, count)
			assertPersistenceUnchanged(t, s, before, s.Path, file, modified)
			if reference != "identity" && s.Data.Nodes["node"].ID != "node" {
				t.Fatal("referenced node was removed")
			}
		})
	}
}

func TestNodePUTServerOwnershipRules(t *testing.T) {
	cases := []struct {
		name, field, reference string
		status                 int
	}{
		{"omitted", "", "", 200},
		{"same", `"srv_a"`, "", 200},
		{"move", `"srv_b"`, "", 200},
		{"trimmed", `" srv_b "`, "", 200},
		{"empty", `""`, "", 400},
		{"whitespace", `"   "`, "", 400},
		{"null", `null`, "", 400},
		{"number", `7`, "", 400},
		{"boolean", `false`, "", 400},
		{"array", `[]`, "", 400},
		{"object", `{}`, "", 400},
		{"missing_server", `"missing"`, "", 400},
		{"client_move", `"srv_b"`, "client", 409},
		{"relay_move", `"srv_b"`, "relay", 409},
		{"client_omitted", "", "client", 200},
		{"relay_same", `"srv_a"`, "relay", 200},
		{"current_identity", `"srv_a"`, "", 409},
		{"server_identity", `"srv_b"`, "", 409},
		{"orphan_current", `"srv_b"`, "", 409},
		{"empty_current", "", "", 409},
		{"null_body", "", "", 400},
		{"uppercase", `"srv_b"`, "", 200},
		{"ambiguous_case", `"srv_b"`, "", 400},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			s := nodeGuardrailFixture(t)
			addNodeGuardrailReference(s, test.reference)
			node := s.Data.Nodes["node"]
			switch test.name {
			case "current_identity":
				stored := node
				stored.ID = "other"
				s.Data.Nodes["node"] = stored
			case "server_identity":
				server := s.Data.Servers["srv_b"]
				server.ID = "other"
				s.Data.Servers["srv_b"] = server
			case "orphan_current":
				delete(s.Data.Servers, "srv_a")
			case "empty_current":
				node.ServerID = ""
				s.Data.Nodes["node"] = node
			}
			node.Name = "After update"
			payload := nodeUpdatePayload(t, node, test.field)
			if test.name == "null_body" {
				payload = "null"
			}
			if test.name == "uppercase" || test.name == "ambiguous_case" {
				var fields map[string]json.RawMessage
				if err := json.Unmarshal([]byte(payload), &fields); err != nil {
					t.Fatal(err)
				}
				fields["SERVER_ID"] = json.RawMessage(`"srv_b"`)
				if test.name == "uppercase" {
					delete(fields, "server_id")
				}
				raw, err := json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				payload = string(raw)
			}
			before := persistenceSnapshot(t, s.Data)
			file, modified := persistDeletionFixture(t, s)
			response := httptest.NewRecorder()
			NewRouter(s).ServeHTTP(response, persistenceRequest(t, "PUT", "/api/nodes/node", payload, false))
			if response.Code != test.status {
				t.Fatalf("expected HTTP %d, got %d: %s", test.status, response.Code, response.Body.String())
			}
			if test.status != http.StatusOK {
				if test.status == http.StatusConflict {
					assertNodeConflictResponse(t, response, 1)
				}
				assertPersistenceUnchanged(t, s, before, s.Path, file, modified)
				return
			}
			wantServer := "srv_a"
			if test.name == "move" || test.name == "trimmed" || test.name == "uppercase" {
				wantServer = "srv_b"
			}
			var result model.Node
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			stored := s.Data.Nodes["node"]
			if result.ID != "node" || result.ServerID != wantServer || result.Name != "After update" || !result.CreatedAt.Equal(before.Nodes["node"].CreatedAt) || !result.CreatedAt.Equal(stored.CreatedAt) || !result.UpdatedAt.Equal(stored.UpdatedAt) {
				t.Fatal("successful PUT lost identity, ownership or expected metadata")
			}
			resultFields, storedFields := result, stored
			resultFields.CreatedAt, resultFields.UpdatedAt = time.Time{}, time.Time{}
			storedFields.CreatedAt, storedFields.UpdatedAt = time.Time{}, time.Time{}
			if !reflect.DeepEqual(resultFields, storedFields) {
				t.Fatal("successful PUT response and stored node differ outside timestamps")
			}
			if !reflect.DeepEqual(before.Clients, s.Data.Clients) || !reflect.DeepEqual(before.RelayRoutes, s.Data.RelayRoutes) || !reflect.DeepEqual(before.Servers, s.Data.Servers) || len(s.Data.OperationLogs) != 1 {
				t.Fatal("successful PUT changed references, servers, BBR or success log count")
			}
			assertNodeFileMatchesMemory(t, s)
		})
	}
}

func TestNodeMutationSaveFailuresReturn500AndPreserveState(t *testing.T) {
	for _, operation := range []string{"PUT", "DELETE"} {
		for _, mode := range []string{"serialization", "create", "rename"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				s := nodeGuardrailFixture(t)
				before := persistenceSnapshot(t, s.Data)
				path := s.Path
				file, modified := persistDeletionFixture(t, s)
				switch mode {
				case "serialization":
					server := s.Data.Servers["srv_a"]
					server.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
					s.Data.Servers["srv_a"], before.Servers["srv_a"] = server, server
				case "create":
					s.Path = filepath.Join(filepath.Dir(path), "missing-parent", "panel.json")
				case "rename":
					s.Path = filepath.Join(filepath.Dir(path), "nonempty-target")
					if err := os.Mkdir(s.Path, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(s.Path, "keep"), file, 0600); err != nil {
						t.Fatal(err)
					}
				}
				node := s.Data.Nodes["node"]
				node.Name = "Not committed"
				payload := nodeUpdatePayload(t, node, `"srv_b"`)
				response := httptest.NewRecorder()
				NewRouter(s).ServeHTTP(response, persistenceRequest(t, operation, "/api/nodes/node", payload, false))
				if response.Code != http.StatusInternalServerError {
					t.Fatalf("failed write returned HTTP %d: %s", response.Code, response.Body.String())
				}
				var result struct {
					Error string `json:"error"`
					OK    bool   `json:"ok"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Error == "" || result.OK || bytes.Contains(response.Body.Bytes(), []byte(filepath.Dir(path))) || bytes.Contains(response.Body.Bytes(), []byte("synthetic-token-")) {
					t.Fatal("write failure returned success or leaked filesystem/credential details")
				}
				assertPersistenceUnchanged(t, s, before, path, file, modified)
			})
		}
	}
}

func TestNodeDeleteUnreferencedCommitAndRepeat(t *testing.T) {
	s := nodeGuardrailFixture(t)
	s.Data.Clients["unbound"] = model.Client{ID: "unbound", Username: "Synthetic unbound", NodeIDs: []string{}, RelayRouteIDs: []string{}}
	s.Data.RelayRoutes["manual"] = model.RelayRoute{ID: "manual", RelayServerID: "srv_b", LandingMode: "manual_socks5", RouteMode: "socks5_route"}
	before := persistenceSnapshot(t, s.Data)
	response := httptest.NewRecorder()
	handler := NewRouter(s)
	handler.ServeHTTP(response, authenticatedSafetyRequest(t, "DELETE", "/api/nodes/node"))
	if response.Code != http.StatusOK {
		t.Fatalf("unreferenced deletion returned HTTP %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || !result.OK {
		t.Fatal("successful deletion contract changed")
	}
	if len(s.Data.Nodes) != 0 || !reflect.DeepEqual(before.Clients, s.Data.Clients) || !reflect.DeepEqual(before.RelayRoutes, s.Data.RelayRoutes) || !reflect.DeepEqual(before.Servers, s.Data.Servers) || len(s.Data.OperationLogs) != 1 {
		t.Fatal("deletion removed unrelated data, modified bindings or lost log")
	}
	assertNodeFileMatchesMemory(t, s)
	before = persistenceSnapshot(t, s.Data)
	// Preserve equivalent log time representation in the independent memory snapshot.
	for id, snapshotLog := range before.OperationLogs {
		liveLog, exists := s.Data.OperationLogs[id]
		if !exists || !snapshotLog.CreatedAt.Equal(liveLog.CreatedAt) {
			t.Fatal("snapshot lost log identity or changed its timestamp")
		}
		snapshotLog.CreatedAt = liveLog.CreatedAt
		before.OperationLogs[id] = snapshotLog
	}
	if !reflect.DeepEqual(before, s.Data) {
		t.Fatal("snapshot differs beyond equivalent log time representation")
	}
	file, modified := persistDeletionFixture(t, s)
	for _, path := range []string{"/api/nodes/node", "/api/nodes/missing"} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, authenticatedSafetyRequest(t, "DELETE", path))
		if response.Code != http.StatusNotFound {
			t.Fatalf("missing target returned HTTP %d", response.Code)
		}
		assertPersistenceUnchanged(t, s, before, s.Path, file, modified)
	}
}

func TestNodeReadEndpointsDoNotSaveOrRepair(t *testing.T) {
	for _, path := range []string{"/api/nodes", "/api/nodes/node"} {
		t.Run(path, func(t *testing.T) {
			s := nodeGuardrailFixture(t)
			node := s.Data.Nodes["node"]
			node.ServerID = "missing"
			s.Data.Nodes["node"] = node
			before := persistenceSnapshot(t, s.Data)
			file, modified := persistDeletionFixture(t, s)
			response := httptest.NewRecorder()
			NewRouter(s).ServeHTTP(response, authenticatedSafetyRequest(t, "GET", path))
			if response.Code != http.StatusOK {
				t.Fatalf("GET returned HTTP %d", response.Code)
			}
			assertPersistenceUnchanged(t, s, before, s.Path, file, modified)
		})
	}
}

func TestNodeMutationConcurrentWithReferenceCreation(t *testing.T) {
	for _, binding := range []string{"client", "relay"} {
		for _, operation := range []string{"DELETE", "PUT"} {
			t.Run(binding+"/"+operation, func(t *testing.T) {
				for round := 0; round < 20; round++ {
					s := nodeGuardrailFixture(t)
					node := s.Data.Nodes["node"]
					bindingPath := "/api/clients"
					bindingPayload := `{"username":"Synthetic concurrent","node_ids":["node"]}`
					if binding == "relay" {
						node.Protocol = "socks"
						node.SocksUsername, node.SocksPassword = "synthetic", "synthetic-only"
						s.Data.Nodes["node"] = node
						bindingPath = "/api/relays"
						bindingPayload = `{"name":"Synthetic concurrent","relay_server_id":"srv_b","relay_port":33001,"landing_node_id":"node","route_mode":"socks5_route","landing_mode":"panel_node","relay_reality_private_key":"synthetic-unused","relay_reality_public_key":"synthetic-unused","relay_reality_short_id":"synthetic-unused"}`
					}
					persistDeletionFixture(t, s)
					requests := []*http.Request{
						persistenceRequest(t, operation, "/api/nodes/node", nodeUpdatePayload(t, node, `"srv_b"`), false),
						persistenceRequest(t, "POST", bindingPath, bindingPayload, false),
					}
					responses := []*httptest.ResponseRecorder{httptest.NewRecorder(), httptest.NewRecorder()}
					handler := NewRouter(s)
					start := make(chan struct{})
					var workers sync.WaitGroup
					for index, request := range requests {
						workers.Add(1)
						go func() {
							defer workers.Done()
							<-start
							handler.ServeHTTP(responses[index], request)
						}()
					}
					close(start)
					workers.Wait()
					mutationCode, bindingCode := responses[0].Code, responses[1].Code
					if operation == "DELETE" {
						if !((mutationCode == 200 && bindingCode == 400) || (mutationCode == 409 && bindingCode == 201)) {
							t.Fatalf("round %d: invalid delete/bind outcome %d/%d", round, mutationCode, bindingCode)
						}
					} else {
						if bindingCode != 201 || (mutationCode != 200 && mutationCode != 409) {
							t.Fatalf("round %d: invalid ownership/bind outcome %d/%d", round, mutationCode, bindingCode)
						}
						wantServer := "srv_a"
						if mutationCode == 200 {
							wantServer = "srv_b"
						}
						if s.Data.Nodes["node"].ServerID != wantServer {
							t.Fatal("ownership change lost reference lock boundary")
						}
					}
					for _, client := range s.Data.Clients {
						for _, nodeID := range client.NodeIDs {
							if _, exists := s.Data.Nodes[nodeID]; !exists {
								t.Fatal("concurrent request created orphan client binding")
							}
						}
					}
					for _, relay := range s.Data.RelayRoutes {
						if relay.LandingNodeID != "" {
							if _, exists := s.Data.Nodes[relay.LandingNodeID]; !exists {
								t.Fatal("concurrent request created orphan landing reference")
							}
						}
					}
					assertNodeFileMatchesMemory(t, s)
				}
			})
		}
	}
}
