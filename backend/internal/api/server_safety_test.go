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
