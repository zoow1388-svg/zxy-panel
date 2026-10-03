// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"zxy-panel/backend/internal/model"
)

func nodeCommitFixture(t *testing.T, referenced bool) *Store {
	t.Helper()
	s := commitFixture(t)
	s.Data.Servers["srv_b"] = model.Server{ID: "srv_b", Name: "Synthetic second server"}
	if !referenced {
		client := s.Data.Clients["client"]
		client.NodeIDs = []string{}
		s.Data.Clients["client"] = client
		relay := s.Data.RelayRoutes["relay"]
		relay.LandingNodeID = ""
		s.Data.RelayRoutes["relay"] = relay
	}
	s.Mu.Lock()
	err := s.SaveLocked()
	s.Mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNodeStoreGuardsCannotBeBypassed(t *testing.T) {
	for _, operation := range []string{"delete", "ownership"} {
		for _, reference := range []string{"client", "relay", "disabled_client", "expired_client", "disabled_relay", "manual_relay", "both"} {
			t.Run(operation+"/"+reference, func(t *testing.T) {
				s := nodeCommitFixture(t, false)
				client := s.Data.Clients["client"]
				relay := s.Data.RelayRoutes["relay"]
				switch reference {
				case "client", "disabled_client", "expired_client", "both":
					client.NodeIDs = []string{"node", "node"}
					client.Enabled = reference != "disabled_client"
					if reference == "expired_client" {
						client.ExpireAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
					}
					s.Data.Clients["client"] = client
				}
				if reference == "relay" || reference == "disabled_relay" || reference == "manual_relay" || reference == "both" {
					relay.LandingNodeID = "node"
					relay.Enabled = reference != "disabled_relay"
					if reference == "manual_relay" {
						relay.RouteMode, relay.LandingMode = "socks5_route", "manual_socks5"
					}
					s.Data.RelayRoutes["relay"] = relay
				}
				before := clonePanelData(s.Data)
				raw, err := os.ReadFile(s.Path)
				if err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(s.Path)
				if err != nil {
					t.Fatal(err)
				}
				node := s.Data.Nodes["node"]
				node.ServerID = "srv_b"
				s.Mu.Lock()
				if operation == "delete" {
					err = s.DeleteNodeLocked("node", "synthetic", "")
				} else {
					err = s.UpdateNodeLocked("node", node, "synthetic", "")
				}
				s.Mu.Unlock()
				var conflict *NodeConflictError
				if !errors.As(err, &conflict) {
					t.Fatalf("expected typed conflict, got %v", err)
				}
				count := 1
				if reference == "both" {
					count = 2
				}
				if len(conflict.Conflicts) != count {
					t.Fatalf("expected %d deduplicated references, got %v", count, conflict.Conflicts)
				}
				if !reflect.DeepEqual(before, s.Data) {
					t.Fatal("guard changed official memory or logs")
				}
				assertCommitFileUnchanged(t, s.Path, raw, info.ModTime())
			})
		}
	}
}

func TestNodeStoreConflictOrder(t *testing.T) {
	s := nodeCommitFixture(t, true)
	s.Data.Clients["a"] = model.Client{ID: "a", NodeIDs: []string{"node", "node"}}
	s.Data.RelayRoutes["a"] = model.RelayRoute{ID: "a", LandingNodeID: "node"}
	want := []string{"client_node_reference/a", "client_node_reference/client", "relay_landing_reference/a", "relay_landing_reference/relay"}
	for round := 0; round < 20; round++ {
		s.Mu.Lock()
		err := s.DeleteNodeLocked("node", "synthetic", "")
		s.Mu.Unlock()
		var conflict *NodeConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("expected conflict: %v", err)
		}
		got := make([]string, 0, len(conflict.Conflicts))
		for _, item := range conflict.Conflicts {
			got = append(got, item.Kind+"/"+item.ResourceID)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("conflicts not sorted or deduplicated: %v", got)
		}
	}
}

func TestNodeStoreIdentityAndParentGuards(t *testing.T) {
	for _, mode := range []string{"missing", "current_id", "proposed_id", "missing_server", "server_id", "empty_server", "orphan_current"} {
		t.Run(mode, func(t *testing.T) {
			s := nodeCommitFixture(t, false)
			node := s.Data.Nodes["node"]
			switch mode {
			case "missing":
				delete(s.Data.Nodes, "node")
			case "current_id":
				current := node
				current.ID = "other"
				s.Data.Nodes["node"] = current
			case "proposed_id":
				node.ID = "other"
			case "missing_server":
				node.ServerID = "missing"
			case "server_id":
				s.Data.Servers["srv_b"] = model.Server{ID: "other"}
				node.ServerID = "srv_b"
			case "empty_server":
				node.ServerID = ""
			case "orphan_current":
				delete(s.Data.Servers, "srv_a")
				node.ServerID = "srv_b"
			}
			before := clonePanelData(s.Data)
			s.Mu.Lock()
			err := s.UpdateNodeLocked("node", node, "synthetic", "")
			s.Mu.Unlock()
			if mode == "missing" {
				if !errors.Is(err, ErrNodeNotFound) {
					t.Fatalf("expected not found: %v", err)
				}
			} else {
				var conflict *NodeConflictError
				if !errors.As(err, &conflict) {
					t.Fatalf("expected conflict: %v", err)
				}
			}
			if !reflect.DeepEqual(before, s.Data) {
				t.Fatal("invalid identity or parent was repaired or committed")
			}
		})
	}
}

func TestNodeStoreCommitFailuresPreserveState(t *testing.T) {
	for _, operation := range []string{"delete", "update"} {
		for _, mode := range []string{"serialization", "create", "rename"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				s := nodeCommitFixture(t, false)
				path := s.Path
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "serialization" {
					server := s.Data.Servers["srv_a"]
					server.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
					s.Data.Servers["srv_a"] = server
				} else if mode == "create" {
					s.Path = filepath.Join(filepath.Dir(path), "missing-parent", "panel.json")
				} else {
					s.Path = filepath.Join(filepath.Dir(path), "nonempty-target")
					if err := os.Mkdir(s.Path, 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(s.Path, "keep"), raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
				before := clonePanelData(s.Data)
				node := s.Data.Nodes["node"]
				node.Name = "after"
				s.Mu.Lock()
				if operation == "delete" {
					err = s.DeleteNodeLocked("node", "synthetic", "")
				} else {
					err = s.UpdateNodeLocked("node", node, "synthetic", "")
				}
				s.Mu.Unlock()
				if err == nil {
					t.Fatal("fault must fail commit")
				}
				if !reflect.DeepEqual(before, s.Data) {
					t.Fatal("failed node commit polluted live data or logs")
				}
				assertCommitFileUnchanged(t, path, raw, info.ModTime())
			})
		}
	}
}

func TestNodeStoreSuccessfulUpdateAndDelete(t *testing.T) {
	s := nodeCommitFixture(t, true)
	clients, relays, servers := clonePanelData(s.Data).Clients, clonePanelData(s.Data).RelayRoutes, clonePanelData(s.Data).Servers
	node := s.Data.Nodes["node"]
	created := node.CreatedAt
	node.Name, node.CreatedAt = "renamed", time.Now()
	s.Mu.Lock()
	err := s.UpdateNodeLocked("node", node, "synthetic", "")
	s.Mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if s.Data.Nodes["node"].Name != "renamed" || !s.Data.Nodes["node"].CreatedAt.Equal(created) {
		t.Fatal("same-owner update lost metadata or creation time")
	}
	if !reflect.DeepEqual(clients, s.Data.Clients) || !reflect.DeepEqual(relays, s.Data.RelayRoutes) || !reflect.DeepEqual(servers, s.Data.Servers) {
		t.Fatal("metadata update modified references or servers")
	}
	s = nodeCommitFixture(t, false)
	node = s.Data.Nodes["node"]
	node.ServerID = "srv_b"
	s.Mu.Lock()
	err = s.UpdateNodeLocked("node", node, "synthetic", "")
	s.Mu.Unlock()
	if err != nil || s.Data.Nodes["node"].ServerID != "srv_b" {
		t.Fatalf("valid unreferenced ownership change failed: %v", err)
	}
	before := clonePanelData(s.Data)
	s.Mu.Lock()
	err = s.DeleteNodeLocked("node", "synthetic", "")
	s.Mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := s.Data.Nodes["node"]; exists {
		t.Fatal("successful deletion retained node")
	}
	if !reflect.DeepEqual(before.Clients, s.Data.Clients) || !reflect.DeepEqual(before.RelayRoutes, s.Data.RelayRoutes) || len(s.Data.OperationLogs) != 2 {
		t.Fatal("deletion unbound resources or lost success logs")
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil || !bytes.Equal(raw, want) {
		t.Fatal("successful commit is not persisted")
	}
	s.Mu.Lock()
	err = s.DeleteNodeLocked("node", "synthetic", "")
	s.Mu.Unlock()
	if !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("repeated deletion must report missing target: %v", err)
	}
}
