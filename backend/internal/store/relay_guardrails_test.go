// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"zxy-panel/backend/internal/model"
)

func relayCommitFixture(t *testing.T, referenced bool) *Store {
	t.Helper()
	s := commitFixture(t)
	s.Data.Servers["srv_b"] = model.Server{ID: "srv_b"}
	s.Data.Nodes["node_b"] = model.Node{ID: "node_b", ServerID: "srv_b"}
	s.Data.RelayRoutes["relay"] = model.RelayRoute{ID: "relay", Name: "Synthetic relay", RelayServerID: "srv_a", RouteMode: "socks5_route", LandingMode: "manual_socks5", ManualSocksHost: "192.0.2.10", ManualSocksPort: 1080, ManualSocksUsername: "synthetic-user", ManualSocksPassword: "synthetic-only"}
	client := s.Data.Clients["client"]
	client.Username, client.NodeIDs, client.RelayRouteIDs = "Synthetic client", []string{}, []string{}
	if referenced {
		client.RelayRouteIDs = []string{"relay", "relay"}
	}
	s.Data.Clients["client"] = client
	s.Mu.Lock()
	err := s.SaveLocked()
	s.Mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func b2StoreEvidence(t *testing.T, s *Store) (model.PanelData, []byte, time.Time) {
	t.Helper()
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	return clonePanelData(s.Data), raw, info.ModTime()
}

func assertB2StoreUnchanged(t *testing.T, s *Store, before model.PanelData, path string, raw []byte, modified time.Time) {
	t.Helper()
	want, got := b2StoreCompareTimes(t, reflect.ValueOf(before), reflect.ValueOf(s.Data))
	if !reflect.DeepEqual(want.Interface(), got.Interface()) {
		t.Fatal("official memory, bindings, BBR or logs changed")
	}
	assertCommitFileUnchanged(t, path, raw, modified)
}

func b2StoreCompareTimes(t *testing.T, a, b reflect.Value) (reflect.Value, reflect.Value) {
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
			u, v := b2StoreCompareTimes(t, a.Field(i), b.Field(i))
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
			u, v := b2StoreCompareTimes(t, a.MapIndex(key), bv)
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
			u, v := b2StoreCompareTimes(t, a.Index(i), b.Index(i))
			x.Index(i).Set(u)
			y.Index(i).Set(v)
		}
		return x, y
	case reflect.Pointer:
		if a.IsNil() || b.IsNil() {
			return a, b
		}
		u, v := b2StoreCompareTimes(t, a.Elem(), b.Elem())
		x, y := reflect.New(a.Type().Elem()), reflect.New(b.Type().Elem())
		x.Elem().Set(u)
		y.Elem().Set(v)
		return x, y
	default:
		return a, b
	}
}

func injectB2StoreFault(t *testing.T, s *Store, mode string) {
	t.Helper()
	switch mode {
	case "serialization":
		server := s.Data.Servers["srv_b"]
		server.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
		s.Data.Servers["srv_b"] = server
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

func TestB2RelayStoreDeleteReferences(t *testing.T) {
	for _, status := range []string{"ordinary", "disabled", "expired", "unreferenced"} {
		t.Run(status, func(t *testing.T) {
			s := relayCommitFixture(t, status != "unreferenced")
			client := s.Data.Clients["client"]
			client.Enabled = status != "disabled"
			if status == "expired" {
				client.ExpireAt = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			s.Data.Clients["client"] = client
			before, raw, modified := b2StoreEvidence(t, s)
			s.Mu.Lock()
			err := s.DeleteRelayLocked("relay", "synthetic", "")
			s.Mu.Unlock()
			if status == "unreferenced" {
				if err != nil || len(s.Data.RelayRoutes) != 0 || len(s.Data.OperationLogs) != 1 {
					t.Fatal("unreferenced delete did not commit")
				}
				if !reflect.DeepEqual(before.Clients, s.Data.Clients) {
					t.Fatal("relay delete changed clients")
				}
				return
			}
			var conflict *BindingConflictError
			if !errors.As(err, &conflict) || len(conflict.Conflicts) != 1 {
				t.Fatal("references must reject deletion once per client")
			}
			assertB2StoreUnchanged(t, s, before, s.Path, raw, modified)
		})
	}
}

func TestB2RelayStoreOwnershipGuards(t *testing.T) {
	changes := map[string]func(*model.RelayRoute){
		"server":   func(r *model.RelayRoute) { r.RelayServerID = "srv_b" },
		"route":    func(r *model.RelayRoute) { r.RouteMode = "tcp_forward"; r.LandingNodeID = "node" },
		"landing":  func(r *model.RelayRoute) { r.LandingMode = "panel_node"; r.LandingNodeID = "node" },
		"node":     func(r *model.RelayRoute) { r.LandingNodeID = "node_b" },
		"host":     func(r *model.RelayRoute) { r.ManualSocksHost = "192.0.2.11" },
		"port":     func(r *model.RelayRoute) { r.ManualSocksPort++ },
		"username": func(r *model.RelayRoute) { r.ManualSocksUsername = "synthetic-other" },
		"password": func(r *model.RelayRoute) { r.ManualSocksPassword = "synthetic-other" },
		"udp":      func(r *model.RelayRoute) { r.ManualSocksUDP = !r.ManualSocksUDP },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			s := relayCommitFixture(t, true)
			before, raw, modified := b2StoreEvidence(t, s)
			relay := s.Data.RelayRoutes["relay"]
			change(&relay)
			s.Mu.Lock()
			err := s.UpdateRelayLocked("relay", relay, "synthetic", "")
			s.Mu.Unlock()
			var conflict *BindingConflictError
			if !errors.As(err, &conflict) {
				t.Fatal("referenced ownership change must conflict")
			}
			assertB2StoreUnchanged(t, s, before, s.Path, raw, modified)
		})
	}
	for _, mode := range []string{"tcp_legacy", "socks_legacy", "ordinary"} {
		t.Run(mode, func(t *testing.T) {
			s := relayCommitFixture(t, true)
			current := s.Data.RelayRoutes["relay"]
			if mode == "tcp_legacy" {
				current.RouteMode = ""
				current.LandingNodeID = "node"
			}
			if mode == "socks_legacy" {
				current.LandingMode = ""
			}
			s.Data.RelayRoutes["relay"] = current
			relay := current
			relay.Name = "After"
			if mode == "tcp_legacy" {
				relay.RouteMode = "tcp_forward"
			}
			if mode == "socks_legacy" {
				relay.LandingMode = "manual_socks5"
			}
			s.Mu.Lock()
			err := s.UpdateRelayLocked("relay", relay, "synthetic", "")
			s.Mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			if s.Data.RelayRoutes["relay"].Name != "After" || len(s.Data.OperationLogs) != 1 {
				t.Fatal("same effective ownership update failed")
			}
		})
	}
}

func TestB2RelayStoreIdentityAndParents(t *testing.T) {
	for _, mode := range []string{"relay_id", "server_id", "missing_server", "node_id", "missing_node", "node_server_id", "node_server_missing", "candidate_id", "duplicate", "missing_object"} {
		t.Run(mode, func(t *testing.T) {
			s := relayCommitFixture(t, false)
			relay := s.Data.RelayRoutes["relay"]
			relay.LandingMode = "panel_node"
			relay.LandingNodeID = "node"
			s.Data.RelayRoutes["relay"] = relay
			switch mode {
			case "relay_id":
				r := relay
				r.ID = "other"
				s.Data.RelayRoutes["relay"] = r
			case "server_id":
				v := s.Data.Servers["srv_a"]
				v.ID = "other"
				s.Data.Servers["srv_a"] = v
			case "missing_server":
				delete(s.Data.Servers, "srv_a")
			case "node_id":
				v := s.Data.Nodes["node"]
				v.ID = "other"
				s.Data.Nodes["node"] = v
			case "missing_node":
				delete(s.Data.Nodes, "node")
			case "node_server_id":
				v := s.Data.Nodes["node"]
				v.ServerID = "srv_b"
				s.Data.Nodes["node"] = v
				v2 := s.Data.Servers["srv_b"]
				v2.ID = "other"
				s.Data.Servers["srv_b"] = v2
			case "node_server_missing":
				v := s.Data.Nodes["node"]
				v.ServerID = "missing"
				s.Data.Nodes["node"] = v
			case "candidate_id":
				relay.ID = "other"
			}
			before, raw, modified := b2StoreEvidence(t, s)
			s.Mu.Lock()
			var err error
			if mode == "duplicate" {
				err = s.CreateRelayLocked(relay, "synthetic", "")
			} else if mode == "missing_object" {
				err = s.DeleteRelayLocked("missing", "synthetic", "")
			} else {
				err = s.UpdateRelayLocked("relay", relay, "synthetic", "")
			}
			s.Mu.Unlock()
			if err == nil {
				t.Fatal("invalid identity or parent accepted")
			}
			if mode == "missing_node" && !errors.Is(err, ErrBindingTargetMissing) {
				t.Fatal("missing binding target must retain bad request classification")
			}
			if mode == "missing_object" && !errors.Is(err, ErrRelayNotFound) {
				t.Fatal("missing operation object must retain not found classification")
			}
			assertB2StoreUnchanged(t, s, before, s.Path, raw, modified)
		})
	}
}

func TestB2RelayStorePersistenceFailures(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete"} {
		for _, mode := range []string{"serialization", "create", "rename"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				s := relayCommitFixture(t, false)
				path := s.Path
				_, raw, modified := b2StoreEvidence(t, s)
				injectB2StoreFault(t, s, mode)
				before := clonePanelData(s.Data)
				relay := s.Data.RelayRoutes["relay"]
				relay.Name = "After"
				s.Mu.Lock()
				var err error
				switch operation {
				case "create":
					relay.ID = "new"
					err = s.CreateRelayLocked(relay, "synthetic", "")
				case "update":
					err = s.UpdateRelayLocked("relay", relay, "synthetic", "")
				case "delete":
					err = s.DeleteRelayLocked("relay", "synthetic", "")
				}
				s.Mu.Unlock()
				if err == nil {
					t.Fatal("injected save failure returned success")
				}
				assertB2StoreUnchanged(t, s, before, path, raw, modified)
			})
		}
	}
}

func TestB2RelayStoreSuccessfulWritesMatchDisk(t *testing.T) {
	s := relayCommitFixture(t, false)
	relay := s.Data.RelayRoutes["relay"]
	relay.ID = "new"
	relay.RelayServerID = "srv_b"
	s.Mu.Lock()
	err := s.CreateRelayLocked(relay, "synthetic", "")
	s.Mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	relay.ManualSocksPassword = "synthetic-next"
	s.Mu.Lock()
	err = s.UpdateRelayLocked("new", relay, "synthetic", "")
	s.Mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil || !reflect.DeepEqual(raw, want) || len(s.Data.OperationLogs) != 2 {
		t.Fatal("successful memory, file or logs differ")
	}
}
