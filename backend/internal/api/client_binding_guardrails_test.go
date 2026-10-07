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
	"strings"
	"testing"
	"time"
)

func TestB2ClientAPIBindingArraySemantics(t *testing.T) {
	for _, name := range []string{"node_ids", "relay_route_ids"} {
		for _, method := range []string{"POST", "PUT"} {
			for _, value := range []string{`null`, `"node"`, `1`, `true`, `{}`, `[1]`, `[""]`, `[" "]`, `[null]`, `["node",null]`} {
				t.Run(method+"/"+name+"/"+value, func(t *testing.T) {
					s := b2APIFixture(t, true)
					c := s.Data.Clients["client"]
					c.NodeIDs = []string{"node"}
					s.Data.Clients[c.ID] = c
					raw, modified := persistDeletionFixture(t, s)
					before := b2APISnapshot(s.Data)
					path := "/api/clients"
					if method == "PUT" {
						path += "/client"
					}
					response := b2APIRequest(t, s, method, path, `{"username":"Synthetic client","`+name+`":`+value+`}`)
					if response.Code != 400 {
						t.Fatalf("invalid array accepted: %d %s", response.Code, response.Body.String())
					}
					b2APIUnchanged(t, s, before, s.Path, raw, modified)
				})
			}
		}
	}
	for _, mode := range []string{"omit_both", "unlink_node", "unlink_relay", "unlink_both", "trim_duplicate", "uppercase", "duplicate_case"} {
		t.Run(mode, func(t *testing.T) {
			s := b2APIFixture(t, true)
			current := s.Data.Clients["client"]
			current.NodeIDs = []string{"node"}
			current.TrafficUsedGB = 17
			s.Data.Clients[current.ID] = current
			raw, modified := persistDeletionFixture(t, s)
			before := b2APISnapshot(s.Data)
			fields := `"username":"After"`
			switch mode {
			case "unlink_node":
				fields += `,"node_ids":[]`
			case "unlink_relay":
				fields += `,"relay_route_ids":[]`
			case "unlink_both":
				fields += `,"node_ids":[],"relay_route_ids":[]`
			case "trim_duplicate":
				fields += `,"node_ids":[" node ","node"],"relay_route_ids":[" relay ","relay"]`
			case "uppercase":
				fields += `,"NODE_IDS":["node"],"RELAY_ROUTE_IDS":["relay"]`
			case "duplicate_case":
				fields += `,"node_ids":[],"NODE_IDS":["node"]`
			}
			response := b2APIRequest(t, s, "PUT", "/api/clients/client", "{"+fields+"}")
			if mode == "duplicate_case" {
				if response.Code != 400 {
					t.Fatal("ambiguous case variants accepted")
				}
				b2APIUnchanged(t, s, before, s.Path, raw, modified)
				return
			}
			if response.Code != 200 {
				t.Fatalf("valid update rejected: %d %s", response.Code, response.Body.String())
			}
			actual := s.Data.Clients["client"]
			wantNodes, wantRelays := []string{"node"}, []string{"relay"}
			if mode == "unlink_node" || mode == "unlink_both" {
				wantNodes = []string{}
			}
			if mode == "unlink_relay" || mode == "unlink_both" {
				wantRelays = []string{}
			}
			if !reflect.DeepEqual(wantNodes, actual.NodeIDs) || !reflect.DeepEqual(wantRelays, actual.RelayRouteIDs) {
				t.Fatal("omission or explicit unlink semantics incorrect")
			}
			if actual.ID != current.ID || actual.UUID != current.UUID || actual.SubscribeToken != current.SubscribeToken || !actual.CreatedAt.Equal(current.CreatedAt) {
				t.Fatal("existing protected client fields changed")
			}
			// Non-binding fields retain the existing full PUT semantics.
			if actual.TrafficUsedGB != 0 || actual.Enabled || actual.Username != "After" {
				t.Fatal("non-binding PUT semantics expanded")
			}
		})
	}
}

func TestB2ClientAPIParentsAndIdentity(t *testing.T) {
	for _, method := range []string{"POST", "PUT"} {
		modes := []string{"node_missing", "node_id", "node_server_missing", "node_server_id", "relay_missing", "relay_id", "relay_server_missing", "relay_server_id", "landing_missing", "landing_id", "landing_server_missing"}
		if method == "PUT" {
			modes = append(modes, "client_id", "missing_object")
		}
		for _, mode := range modes {
			t.Run(method+"/"+mode, func(t *testing.T) {
				s := b2APIFixture(t, false)
				relay := s.Data.RelayRoutes["relay"]
				relay.LandingMode = "panel_node"
				relay.LandingNodeID = "node_b"
				s.Data.RelayRoutes[relay.ID] = relay
				switch mode {
				case "node_missing":
					delete(s.Data.Nodes, "node")
				case "node_id":
					v := s.Data.Nodes["node"]
					v.ID = "other"
					s.Data.Nodes["node"] = v
				case "node_server_missing":
					v := s.Data.Nodes["node"]
					v.ServerID = "missing"
					s.Data.Nodes["node"] = v
				case "node_server_id":
					v := s.Data.Servers["srv_a"]
					v.ID = "other"
					s.Data.Servers["srv_a"] = v
				case "relay_missing":
					delete(s.Data.RelayRoutes, "relay")
				case "relay_id":
					v := relay
					v.ID = "other"
					s.Data.RelayRoutes["relay"] = v
				case "relay_server_missing":
					v := relay
					v.RelayServerID = "missing"
					s.Data.RelayRoutes["relay"] = v
				case "relay_server_id":
					v := s.Data.Servers["srv_b"]
					v.ID = "other"
					s.Data.Servers["srv_b"] = v
				case "landing_missing":
					delete(s.Data.Nodes, "node_b")
				case "landing_id":
					v := s.Data.Nodes["node_b"]
					v.ID = "other"
					s.Data.Nodes["node_b"] = v
				case "landing_server_missing":
					v := s.Data.Nodes["node_b"]
					v.ServerID = "missing"
					s.Data.Nodes["node_b"] = v
				case "client_id":
					v := s.Data.Clients["client"]
					v.ID = "other"
					s.Data.Clients["client"] = v
				}
				raw, modified := persistDeletionFixture(t, s)
				before := b2APISnapshot(s.Data)
				path := "/api/clients"
				if method == "PUT" {
					path += "/client"
					if mode == "missing_object" {
						path = "/api/clients/missing"
					}
				}
				response := b2APIRequest(t, s, method, path, `{"username":"After","node_ids":["node"],"relay_route_ids":["relay"]}`)
				want := 409
				if mode == "node_missing" || mode == "relay_missing" || mode == "landing_missing" {
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
}

func TestB2ClientAPIPersistenceFailuresKeepReferences(t *testing.T) {
	for _, operation := range []string{"POST", "PUT", "UNLINK", "DELETE", "COMBINATION"} {
		for _, mode := range []string{"serialization", "create", "rename"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				s := b2APIFixture(t, true)
				c := s.Data.Clients["client"]
				c.NodeIDs = []string{"node"}
				s.Data.Clients[c.ID] = c
				path := s.Path
				raw, modified := persistDeletionFixture(t, s)
				b2APIFault(t, s, mode)
				before := b2APISnapshot(s.Data)
				url := "/api/clients/client"
				method := operation
				payload := `{"username":"After","node_ids":["node"],"relay_route_ids":["relay"]}`
				switch operation {
				case "POST":
					url = "/api/clients"
				case "UNLINK":
					method = "PUT"
					payload = `{"username":"After","node_ids":[],"relay_route_ids":[]}`
				case "COMBINATION":
					method = "POST"
					url = "/api/clients/create-socks5-relay"
					payload = `{"username":"Synthetic new","landing_exit_id":"exit","relay_server_id":"srv_b","relay_port":33002}`
				}
				response := b2APIRequest(t, s, method, url, payload)
				if response.Code != 500 {
					t.Fatalf("save failure returned %d: %s", response.Code, response.Body.String())
				}
				if strings.Contains(response.Body.String(), filepath.Dir(path)) || strings.Contains(response.Body.String(), "synthetic-only") || strings.Contains(response.Body.String(), "synthetic-token") {
					t.Fatal("failure leaked credentials or path")
				}
				b2APIUnchanged(t, s, before, path, raw, modified)
				if operation == "UNLINK" || operation == "DELETE" {
					for _, target := range []string{"/api/nodes/node", "/api/relays/relay"} {
						if result := b2APIRequest(t, s, "DELETE", target, ""); result.Code != 409 {
							t.Fatalf("failed release permitted later deletion: %d", result.Code)
						}
					}
					b2APIUnchanged(t, s, before, path, raw, modified)
				}
			})
		}
	}
}

func TestB2ClientAPICombinationSuccessAndSnapshot(t *testing.T) {
	s := b2APIFixture(t, false)
	before := b2APISnapshot(s.Data)
	response := b2APIRequest(t, s, "POST", "/api/clients/create-socks5-relay", `{"username":"Synthetic new","landing_exit_id":"exit","relay_server_id":"srv_b","relay_port":33002}`)
	if response.Code != 201 {
		t.Fatalf("combination failed: %d %s", response.Code, response.Body.String())
	}
	var result createClientRelayResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Client.ID == "" || result.Relay.ID == "" || result.Client.UUID == "" || result.Client.SubscribeToken == "" || !result.Client.Enabled || result.Client.TrafficLimitGB != 100 || !result.Client.ExpireAt.IsZero() {
		t.Fatal("existing client generation changed")
	}
	if !reflect.DeepEqual(result.Client.RelayRouteIDs, []string{result.Relay.ID}) || result.Relay.LandingMode != "manual_socks5" || result.Relay.LandingNodeID != "" || result.Relay.RouteMode != "socks5_route" || result.Relay.RelayNetwork != "tcp" {
		t.Fatal("existing combination binding changed")
	}
	exit := before.LandingExits["exit"]
	if result.Relay.ManualSocksHost != exit.Host || result.Relay.ManualSocksPort != exit.Port || result.Relay.ManualSocksUsername != exit.Username || result.Relay.ManualSocksPassword != exit.Password || result.Relay.ManualSocksUDP != exit.UDP {
		t.Fatal("manual SOCKS snapshot changed")
	}
	if result.Relay.RelayRealityPrivateKey == "" || result.Relay.RelayRealityPublicKey == "" || result.Relay.RelayRealityShortID == "" || result.Relay.RelayRealityDest != "www.intel.com:443" || result.Relay.RelayRealitySpiderX != "/" {
		t.Fatal("existing configuration generation changed")
	}
	if len(s.Data.Clients) != 2 || len(s.Data.RelayRoutes) != 2 || len(s.Data.OperationLogs) != 1 {
		t.Fatal("combination was not one complete commit")
	}
	changed := s.Data.LandingExits["exit"]
	changed.Host = "192.0.2.99"
	s.Data.LandingExits["exit"] = changed
	if s.Data.RelayRoutes[result.Relay.ID].ManualSocksHost != exit.Host {
		t.Fatal("manual snapshot became a live LandingExit relationship")
	}
	// Restore only synthetic test data before checking the committed file.
	s.Data.LandingExits["exit"] = exit
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil || !bytes.Equal(raw, want) {
		t.Fatal("committed memory and disk differ")
	}
	stored := s.Data.Clients[result.Client.ID]
	if !stored.CreatedAt.Equal(result.Client.CreatedAt) || !stored.UpdatedAt.Equal(result.Client.UpdatedAt) || !stored.ExpireAt.Equal(result.Client.ExpireAt) {
		t.Fatal("response client times differ")
	}
	a, b := stored, result.Client
	a.CreatedAt, a.UpdatedAt, a.ExpireAt = time.Time{}, time.Time{}, time.Time{}
	b.CreatedAt, b.UpdatedAt, b.ExpireAt = time.Time{}, time.Time{}, time.Time{}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("response client non-time fields differ")
	}
}

func TestB2ClientAPICombinationIdentity(t *testing.T) {
	for _, mode := range []string{"exit_id", "server_id", "missing_server"} {
		t.Run(mode, func(t *testing.T) {
			s := b2APIFixture(t, false)
			switch mode {
			case "exit_id":
				v := s.Data.LandingExits["exit"]
				v.ID = "other"
				s.Data.LandingExits["exit"] = v
			case "server_id":
				v := s.Data.Servers["srv_b"]
				v.ID = "other"
				s.Data.Servers["srv_b"] = v
			case "missing_server":
				delete(s.Data.Servers, "srv_b")
			}
			raw, modified := persistDeletionFixture(t, s)
			before := b2APISnapshot(s.Data)
			response := b2APIRequest(t, s, "POST", "/api/clients/create-socks5-relay", `{"username":"Synthetic new","landing_exit_id":"exit","relay_server_id":"srv_b","relay_port":33002}`)
			if response.Code != 409 {
				t.Fatalf("invalid identity accepted: %d %s", response.Code, response.Body.String())
			}
			b2APIUnchanged(t, s, before, s.Path, raw, modified)
		})
	}
}

type b2LockedResponse struct {
	*httptest.ResponseRecorder
	reached, release chan struct{}
}

func (w *b2LockedResponse) WriteHeader(status int) {
	w.ResponseRecorder.WriteHeader(status)
	close(w.reached)
	<-w.release
}

func TestB2ClientAPIBindingMutationDeterministicLockOrders(t *testing.T) {
	for _, operation := range []string{"delete_relay", "ownership", "delete_node", "relay_landing"} {
		for _, bindFirst := range []bool{false, true} {
			t.Run(operation+map[bool]string{true: "/bind_first", false: "/mutation_first"}[bindFirst], func(t *testing.T) {
				s := b2APIFixture(t, false)
				handler := NewRouter(s)
				bindMethod, bindPath, bindPayload := "POST", "/api/clients", `{"username":"Synthetic new","relay_route_ids":["relay"]}`
				mutationMethod, mutationPath, mutationPayload := "DELETE", "/api/relays/relay", ""
				if operation == "ownership" {
					mutationMethod = "PUT"
					mutationPayload = b2RelayPayload(t, s.Data.RelayRoutes["relay"], map[string]any{"manual_socks_password": "synthetic-next"})
				}
				if operation == "delete_node" {
					bindPayload = `{"username":"Synthetic new","node_ids":["node"]}`
					mutationPath = "/api/nodes/node"
				}
				if operation == "relay_landing" {
					bindPath = "/api/relays"
					relay := s.Data.RelayRoutes["relay"]
					relay.LandingMode = "panel_node"
					relay.LandingNodeID = "node"
					relay.RelayPort++
					bindPayload = b2RelayPayload(t, relay, nil)
					mutationPath = "/api/nodes/node"
				}
				first := persistenceRequest(t, mutationMethod, mutationPath, mutationPayload, false)
				second := persistenceRequest(t, bindMethod, bindPath, bindPayload, false)
				if bindFirst {
					first, second = second, first
				}
				gate := &b2LockedResponse{ResponseRecorder: httptest.NewRecorder(), reached: make(chan struct{}), release: make(chan struct{})}
				firstDone := make(chan struct{})
				secondDone := make(chan struct{})
				go func() { handler.ServeHTTP(gate, first); close(firstDone) }()
				<-gate.reached
				if s.Mu.TryLock() {
					s.Mu.Unlock()
					close(gate.release)
					<-firstDone
					t.Fatal("first response no longer holds the transaction lock")
				}
				other := httptest.NewRecorder()
				attempted := make(chan struct{})
				go func() { close(attempted); handler.ServeHTTP(other, second); close(secondDone) }()
				<-attempted
				close(gate.release)
				<-firstDone
				<-secondDone
				wantFirst, wantSecond := 200, 400
				if bindFirst {
					wantFirst, wantSecond = 201, 409
				}
				if operation == "ownership" && !bindFirst {
					wantSecond = 201
				}
				if gate.Code != wantFirst || other.Code != wantSecond {
					t.Fatalf("wrong deterministic order outcomes: %d/%d wanted %d/%d", gate.Code, other.Code, wantFirst, wantSecond)
				}
				for _, client := range s.Data.Clients {
					for _, id := range client.NodeIDs {
						if _, ok := s.Data.Nodes[id]; !ok {
							t.Fatal("dangling Node binding")
						}
					}
					for _, id := range client.RelayRouteIDs {
						if _, ok := s.Data.RelayRoutes[id]; !ok {
							t.Fatal("dangling Relay binding")
						}
					}
				}
				for _, relay := range s.Data.RelayRoutes {
					if relay.LandingNodeID != "" {
						if _, ok := s.Data.Nodes[relay.LandingNodeID]; !ok {
							t.Fatal("dangling landing Node reference")
						}
					}
				}
				raw, err := os.ReadFile(s.Path)
				if err != nil {
					t.Fatal(err)
				}
				want, err := json.MarshalIndent(s.Data, "", "  ")
				if err != nil || !bytes.Equal(raw, want) {
					t.Fatal("concurrent memory and file differ")
				}
			})
		}
	}
}

func TestB2ClientAPIReadAndDeleteIdentity(t *testing.T) {
	s := b2APIFixture(t, true)
	raw, modified := persistDeletionFixture(t, s)
	before := b2APISnapshot(s.Data)
	for _, path := range []string{"/api/clients", "/api/clients/client"} {
		if response := b2APIRequest(t, s, http.MethodGet, path, ""); response.Code != 200 {
			t.Fatal("read failed")
		}
	}
	b2APIUnchanged(t, s, before, s.Path, raw, modified)
	client := s.Data.Clients["client"]
	client.ID = "other"
	s.Data.Clients["client"] = client
	raw, modified = persistDeletionFixture(t, s)
	before = b2APISnapshot(s.Data)
	if response := b2APIRequest(t, s, "DELETE", "/api/clients/client", ""); response.Code != 409 {
		t.Fatal("inconsistent client delete accepted")
	}
	b2APIUnchanged(t, s, before, s.Path, raw, modified)
	if response := b2APIRequest(t, s, "DELETE", "/api/clients/missing", ""); response.Code != 404 {
		t.Fatal("missing client delete must return 404")
	}
}
