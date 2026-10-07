// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestB2ClientStoreBindingsAndParents(t *testing.T) {
	for _, operation := range []string{"create", "update", "combination"} {
		for _, mode := range []string{"node_missing", "node_id", "node_server_missing", "node_server_id", "relay_missing", "relay_id", "relay_server_missing", "relay_server_id", "landing_missing", "landing_id", "landing_server_missing", "client_id", "duplicate", "empty_node", "empty_relay"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				s := relayCommitFixture(t, false)
				relay := s.Data.RelayRoutes["relay"]
				relay.LandingMode = "panel_node"
				relay.LandingNodeID = "node_b"
				s.Data.RelayRoutes["relay"] = relay
				client := s.Data.Clients["client"]
				client.NodeIDs = []string{"node"}
				client.RelayRouteIDs = []string{"relay"}
				if operation != "update" {
					client.ID = "new-client"
				}
				if operation == "combination" {
					relay.ID = "new-relay"
					client.RelayRouteIDs = []string{relay.ID}
				}
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
					client.RelayRouteIDs = []string{"missing"}
				case "relay_id":
					if operation == "combination" {
						relay.ID = ""
					} else {
						v := s.Data.RelayRoutes["relay"]
						v.ID = "other"
						s.Data.RelayRoutes["relay"] = v
					}
				case "relay_server_missing":
					relay.RelayServerID = "missing"
					if operation != "combination" {
						s.Data.RelayRoutes["relay"] = relay
					}
				case "relay_server_id":
					v := s.Data.Servers["srv_a"]
					v.ID = "other"
					s.Data.Servers["srv_a"] = v
				case "landing_missing":
					delete(s.Data.Nodes, "node_b")
				case "landing_id":
					v := s.Data.Nodes["node_b"]
					v.ID = "other"
					s.Data.Nodes["node_b"] = v
				case "landing_server_missing":
					delete(s.Data.Servers, "srv_b")
				case "client_id":
					if operation == "update" {
						v := s.Data.Clients["client"]
						v.ID = "other"
						s.Data.Clients["client"] = v
					} else {
						client.ID = ""
					}
				case "duplicate":
					if operation == "update" {
						client.ID = "other"
					} else {
						client.ID = "client"
					}
				case "empty_node":
					client.NodeIDs = []string{" "}
				case "empty_relay":
					client.RelayRouteIDs = []string{""}
				}
				before, raw, modified := b2StoreEvidence(t, s)
				s.Mu.Lock()
				var err error
				switch operation {
				case "create":
					err = s.CreateClientLocked(client, "synthetic", "")
				case "update":
					err = s.UpdateClientLocked("client", client, "synthetic", "")
				case "combination":
					err = s.CreateClientRelayLocked(client, relay, "synthetic", "", "synthetic")
				}
				s.Mu.Unlock()
				if err == nil {
					t.Fatal("invalid identity or parent accepted")
				}
				assertB2StoreUnchanged(t, s, before, s.Path, raw, modified)
			})
		}
	}
}

func TestB2ClientStorePersistenceFailuresKeepReferences(t *testing.T) {
	for _, operation := range []string{"create", "update", "unlink", "delete", "combination"} {
		for _, mode := range []string{"serialization", "create", "rename"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				s := relayCommitFixture(t, true)
				client := s.Data.Clients["client"]
				client.NodeIDs = []string{"node"}
				s.Data.Clients["client"] = client
				path := s.Path
				_, raw, modified := b2StoreEvidence(t, s)
				injectB2StoreFault(t, s, mode)
				before := clonePanelData(s.Data)
				client.Username = "After"
				relay := s.Data.RelayRoutes["relay"]
				s.Mu.Lock()
				var err error
				switch operation {
				case "create":
					client.ID = "new-client"
					err = s.CreateClientLocked(client, "synthetic", "")
				case "update":
					err = s.UpdateClientLocked("client", client, "synthetic", "")
				case "unlink":
					client.NodeIDs = []string{}
					client.RelayRouteIDs = []string{}
					err = s.UpdateClientLocked("client", client, "synthetic", "")
				case "delete":
					err = s.DeleteClientLocked("client", "synthetic", "")
				case "combination":
					relay.ID = "new-relay"
					client.ID = "new-client"
					client.RelayRouteIDs = []string{relay.ID}
					err = s.CreateClientRelayLocked(client, relay, "synthetic", "", "synthetic")
				}
				s.Mu.Unlock()
				if err == nil {
					t.Fatal("injected save failure returned success")
				}
				assertB2StoreUnchanged(t, s, before, path, raw, modified)
				if operation == "unlink" || operation == "delete" {
					s.Mu.Lock()
					nodeErr := s.DeleteNodeLocked("node", "synthetic", "")
					relayErr := s.DeleteRelayLocked("relay", "synthetic", "")
					s.Mu.Unlock()
					var nodeConflict *NodeConflictError
					var relayConflict *BindingConflictError
					if !errors.As(nodeErr, &nodeConflict) || !errors.As(relayErr, &relayConflict) {
						t.Fatal("failed release allowed later Node or Relay deletion")
					}
					assertB2StoreUnchanged(t, s, before, path, raw, modified)
				}
			})
		}
	}
}

func TestB2ClientStoreOwnsAllInputSlices(t *testing.T) {
	for _, operation := range []string{"create", "update", "combination"} {
		t.Run(operation, func(t *testing.T) {
			s := relayCommitFixture(t, false)
			client := s.Data.Clients["client"]
			client.NodeIDs = []string{"node"}
			client.RelayRouteIDs = []string{"relay"}
			relay := s.Data.RelayRoutes["relay"]
			if operation != "update" {
				client.ID = "new-client"
			}
			if operation == "combination" {
				relay.ID = "new-relay"
				client.RelayRouteIDs = []string{relay.ID}
			}
			s.Mu.Lock()
			var err error
			switch operation {
			case "create":
				err = s.CreateClientLocked(client, "synthetic", "")
			case "update":
				err = s.UpdateClientLocked("client", client, "synthetic", "")
			case "combination":
				err = s.CreateClientRelayLocked(client, relay, "synthetic", "", "synthetic")
			}
			s.Mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			before, raw, modified := b2StoreEvidence(t, s)
			client.NodeIDs[0] = "outside-node"
			client.RelayRouteIDs[0] = "outside-relay"
			assertB2StoreUnchanged(t, s, before, s.Path, raw, modified)
			if len(s.Data.OperationLogs) != 1 {
				t.Fatal("operation must commit exactly one success log")
			}
			expected, err := json.MarshalIndent(s.Data, "", "  ")
			if err != nil || !reflect.DeepEqual(raw, expected) {
				t.Fatal("success memory and disk differ")
			}
			if operation == "combination" {
				if _, ok := s.Data.RelayRoutes["new-relay"]; !ok {
					t.Fatal("combination omitted relay")
				}
			}
		})
	}
}

func TestB2ClientStoreDeleteIdentityAndSuccess(t *testing.T) {
	for _, mode := range []string{"missing", "inconsistent", "success"} {
		t.Run(mode, func(t *testing.T) {
			s := relayCommitFixture(t, true)
			id := "client"
			if mode == "missing" {
				id = "missing"
			}
			if mode == "inconsistent" {
				v := s.Data.Clients[id]
				v.ID = "other"
				s.Data.Clients[id] = v
			}
			before, raw, modified := b2StoreEvidence(t, s)
			s.Mu.Lock()
			err := s.DeleteClientLocked(id, "synthetic", "")
			s.Mu.Unlock()
			if mode == "success" {
				if err != nil || len(s.Data.Clients) != 0 || !reflect.DeepEqual(before.RelayRoutes, s.Data.RelayRoutes) {
					t.Fatal("client delete did not commit or modified relay")
				}
				return
			}
			if err == nil {
				t.Fatal("invalid client delete accepted")
			}
			if mode == "missing" && !errors.Is(err, ErrClientNotFound) {
				t.Fatal("missing client must be not found")
			}
			assertB2StoreUnchanged(t, s, before, s.Path, raw, modified)
		})
	}
}

func TestB2StoreBindingDeleteAndOwnershipLockOrders(t *testing.T) {
	for _, operation := range []string{"delete", "ownership"} {
		for _, bindFirst := range []bool{false, true} {
			t.Run(operation+map[bool]string{true: "/bind_first", false: "/mutation_first"}[bindFirst], func(t *testing.T) {
				s := relayCommitFixture(t, false)
				client := s.Data.Clients["client"]
				client.ID = "new-client"
				client.RelayRouteIDs = []string{"relay"}
				relay := s.Data.RelayRoutes["relay"]
				relay.RelayServerID = "srv_b"
				bind := func() error { return s.CreateClientLocked(client, "synthetic", "") }
				mutate := func() error {
					if operation == "delete" {
						return s.DeleteRelayLocked("relay", "synthetic", "")
					}
					return s.UpdateRelayLocked("relay", relay, "synthetic", "")
				}
				first, second := mutate, bind
				if bindFirst {
					first, second = bind, mutate
				}
				// Hold the first transaction's lock before starting the contending writer.
				s.Mu.Lock()
				attempted := make(chan struct{})
				done := make(chan error, 1)
				go func() { close(attempted); s.Mu.Lock(); defer s.Mu.Unlock(); done <- second() }()
				<-attempted
				firstErr := first()
				s.Mu.Unlock()
				secondErr := <-done
				if firstErr != nil {
					t.Fatal(firstErr)
				}
				if bindFirst {
					var conflict *BindingConflictError
					if !errors.As(secondErr, &conflict) {
						t.Fatal("binding-first mutation must conflict")
					}
				} else if operation == "delete" {
					if !errors.Is(secondErr, ErrBindingTargetMissing) {
						t.Fatal("delete-first binding must reject missing target")
					}
				} else if secondErr != nil {
					t.Fatal(secondErr)
				}
				for _, c := range s.Data.Clients {
					for _, rid := range c.RelayRouteIDs {
						if _, ok := s.Data.RelayRoutes[rid]; !ok {
							t.Fatal("dangling relay reference")
						}
					}
				}
				if operation == "ownership" && !bindFirst && s.Data.RelayRoutes["relay"].RelayServerID != "srv_b" {
					t.Fatal("binding did not observe committed new ownership")
				}
				raw, err := os.ReadFile(s.Path)
				if err != nil {
					t.Fatal(err)
				}
				expected, err := json.MarshalIndent(s.Data, "", "  ")
				if err != nil || !reflect.DeepEqual(raw, expected) {
					t.Fatal("concurrent committed memory and disk differ")
				}
			})
		}
	}
}
