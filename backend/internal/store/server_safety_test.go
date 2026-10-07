// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"zxy-panel/backend/internal/model"
)

func safetyServer(id, version, ip string) model.Server {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	return model.Server{
		ID: id, Name: "Synthetic " + id, IP: ip, Host: ip,
		Region: "TEST", Provider: "Synthetic-only", Status: "online",
		AgentToken: "synthetic-token-" + id, AgentVersion: version,
		LastSyncAt: now, CreatedAt: now, UpdatedAt: now,
	}
}

func safetyData(servers ...model.Server) model.PanelData {
	d := model.PanelData{
		Version: "0.7.8-stable-engineering",
		Admins:  map[string]model.AdminUser{}, Servers: map[string]model.Server{},
		Nodes: map[string]model.Node{}, Clients: map[string]model.Client{},
		RelayRoutes: map[string]model.RelayRoute{}, LandingExits: map[string]model.LandingExit{},
		OperationLogs: map[string]model.OperationLog{},
		NetworkPolicy: defaultNetworkPolicy(), NetworkPolicyBackup: defaultNetworkPolicy(),
	}
	for _, server := range servers {
		d.Servers[server.ID] = server
	}
	return d
}

func safetyFile(t *testing.T, raw []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "synthetic-panel.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	fixedTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(path, fixedTime, fixedTime); err != nil {
		t.Fatal(err)
	}
	return path
}

func safetyJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertSafetyFileUnchanged(t *testing.T, path string, before []byte, modTime time.Time) {
	t.Helper()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("loading existing data changed the persisted JSON")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(modTime) {
		t.Error("loading existing data rewrote the database file")
	}
}

func TestLoadExistingServersDoesNotMergeOrMigrate(t *testing.T) {
	t.Setenv("ZXY_LOCAL_SERVER_IP", "192.0.2.10")
	t.Setenv("ZXY_LOCAL_SERVER_HOST", "192.0.2.10")
	a := safetyServer("srv_a", "0.7.8-stable-engineering", "192.0.2.10")
	b := safetyServer("srv_b", "0.7.7.6-bbr-optimization-agent-xray", "192.0.2.10")
	b.Status = "offline"
	sameVersion := b
	sameVersion.AgentVersion = a.AgentVersion
	differentEndpoint := b
	differentEndpoint.IP, differentEndpoint.Host = "192.0.2.20", "192.0.2.20"
	remoteA, remoteB := a, b
	remoteA.IP, remoteA.Host = "192.0.2.20", "192.0.2.20"
	remoteB.IP, remoteB.Host = "192.0.2.30", "192.0.2.30"
	loopback := b
	loopback.IP, loopback.Host = "127.0.0.1", "localhost"
	partial := model.Server{ID: "srv_partial", IP: "127.0.0.1", Host: "localhost"}
	cases := []struct {
		name    string
		servers []model.Server
	}{
		{"Case_A_different_versions", []model.Server{a, b}},
		{"Case_B_same_version_distinct_identities", []model.Server{a, sameVersion}},
		{"Case_C_relay_references", []model.Server{a, b}},
		{"different_endpoint_control", []model.Server{a, differentEndpoint}},
		{"no_local_candidate", []model.Server{remoteA, remoteB}},
		{"loopback_is_not_identity", []model.Server{a, loopback}},
		{"single_partial_identity_is_not_rewritten", []model.Server{partial}},
		{"existing_empty_server_collection", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := safetyData(tc.servers...)
			for i, server := range tc.servers {
				nodeID := "node_" + server.ID
				relayID := "relay_" + server.ID
				before.Nodes[nodeID] = model.Node{ID: nodeID, ServerID: server.ID, Port: 31001 + i, Enabled: true}
				before.RelayRoutes[relayID] = model.RelayRoute{
					ID: relayID, RelayServerID: server.ID, LandingNodeID: nodeID,
					RelayHost: "relay.invalid", RelayPort: 32001 + i, RouteMode: "tcp_forward", Enabled: true,
				}
				before.Clients["client_"+server.ID] = model.Client{
					ID: "client_" + server.ID, NodeIDs: []string{nodeID}, RelayRouteIDs: []string{relayID}, Enabled: true,
				}
			}
			raw := safetyJSON(t, before)
			path := safetyFile(t, raw)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 3; attempt++ {
				s, err := Open(path)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(s.Data.Servers, before.Servers) {
					t.Errorf("load %d changed server identities: before=%d after=%d", attempt, len(before.Servers), len(s.Data.Servers))
				}
				if !reflect.DeepEqual(s.Data.Nodes, before.Nodes) {
					t.Errorf("load %d migrated node ownership", attempt)
				}
				if !reflect.DeepEqual(s.Data.RelayRoutes, before.RelayRoutes) || !reflect.DeepEqual(s.Data.Clients, before.Clients) {
					t.Errorf("load %d changed relay/client references", attempt)
				}
				for _, relay := range s.Data.RelayRoutes {
					if _, exists := s.Data.Servers[relay.RelayServerID]; !exists {
						t.Errorf("load %d orphaned relay %s", attempt, relay.ID)
					}
				}
				assertSafetyFileUnchanged(t, path, raw, info.ModTime())
			}
		})
	}
}

func TestLoadDoesNotRepairHistoricalOrphan(t *testing.T) {
	before := safetyData(safetyServer("srv_a", "0.7.8-stable-engineering", "192.0.2.10"))
	before.RelayRoutes["relay_orphan"] = model.RelayRoute{
		ID: "relay_orphan", RelayServerID: "srv_missing", Enabled: true,
	}
	raw := safetyJSON(t, before)
	path := safetyFile(t, raw)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Data.Servers, before.Servers) || !reflect.DeepEqual(s.Data.RelayRoutes, before.RelayRoutes) {
		t.Error("historical orphan was automatically repaired or rebound")
	}
	assertSafetyFileUnchanged(t, path, raw, info.ModTime())
}

func TestLoadRejectsEmptyOrMalformedExistingFileWithoutReinitializing(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("{"), []byte("null")} {
		t.Run(stringName(raw), func(t *testing.T) {
			path := safetyFile(t, raw)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path); err == nil {
				t.Error("invalid existing database must not be accepted or initialized as a new installation")
			}
			assertSafetyFileUnchanged(t, path, raw, info.ModTime())
		})
	}
}

func stringName(raw []byte) string {
	if len(raw) == 0 {
		return "empty"
	}
	if string(raw) == "null" {
		return "null"
	}
	return "malformed"
}

func TestNewDatabaseStillInitializesOneLocalServer(t *testing.T) {
	t.Setenv("ZXY_LOCAL_SERVER_IP", "192.0.2.10")
	t.Setenv("ZXY_LOCAL_SERVER_HOST", "local.invalid")
	t.Setenv("ZXY_LOCAL_SERVER_NAME", "Synthetic local server")
	t.Setenv("ZXY_ADMIN_USERNAME", "synthetic-admin")
	t.Setenv("ZXY_ADMIN_PASSWORD", "synthetic-fixture-password-not-production")
	path := filepath.Join(t.TempDir(), "new-panel.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Data.Servers) != 1 || len(s.Data.Admins) != 1 {
		t.Fatalf("new installation should initialize one server/admin; got %d/%d", len(s.Data.Servers), len(s.Data.Admins))
	}
	for _, server := range s.Data.Servers {
		if server.ID == "" || server.AgentToken == "" || server.IP != "192.0.2.10" || server.Host != "local.invalid" {
			t.Error("new local server initialization is incomplete")
		}
	}
	reloaded, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(safetyJSON(t, reloaded.Data.Servers), safetyJSON(t, s.Data.Servers)) {
		t.Error("reloading a new installation changed its server identity")
	}
}
