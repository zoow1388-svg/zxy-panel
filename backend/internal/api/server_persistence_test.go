// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"zxy-panel/backend/internal/model"
	"zxy-panel/backend/internal/store"
	"zxy-panel/backend/internal/xray"
)

func TestUpdateVersionReadLockReleasedBeforeExternalLookup(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "updates.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Recv == nil || function.Name.Name != "currentXrayVersionText" {
			continue
		}
		lock, unlock, traversal, fallback := -1, -1, -1, -1
		for index, statement := range function.Body.List {
			switch statement := statement.(type) {
			case *ast.ExprStmt:
				if call, ok := statement.X.(*ast.CallExpr); ok {
					if callsStoreLock(call, "RLock") {
						lock = index
					}
					if callsStoreLock(call, "RUnlock") {
						unlock = index
					}
				}
			case *ast.RangeStmt:
				traversal = index
			case *ast.ReturnStmt:
				for _, value := range statement.Results {
					if call, ok := value.(*ast.CallExpr); ok {
						if name, ok := call.Fun.(*ast.Ident); ok && name.Name == "currentXrayVersionText" {
							fallback = index
						}
					}
				}
			}
		}
		if lock < 0 || traversal <= lock || unlock <= traversal || fallback <= unlock {
			t.Fatal("version traversal must hold RLock and release it before external lookup")
		}
		return
	}
	t.Fatal("Router version lookup not found")
}

func TestUpdateVersionConcurrentWithServerWrites(t *testing.T) {
	s := deletionSafetyFixture(t)
	server := s.Data.Servers["srv_b"]
	server.XrayVersion = "synthetic-a"
	s.Data.Servers["srv_b"] = server
	r := &Router{store: s}
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(5)
	go func() {
		defer workers.Done()
		<-start
		for round := 0; round < 500; round++ {
			s.Mu.Lock()
			server := s.Data.Servers["srv_b"]
			server.XrayVersion = "synthetic-b"
			if round%2 == 0 {
				server.XrayVersion = "synthetic-a"
			}
			s.Data.Servers["srv_b"] = server
			s.Mu.Unlock()
		}
	}()
	for reader := 0; reader < 4; reader++ {
		go func() {
			defer workers.Done()
			<-start
			for round := 0; round < 500; round++ {
				version := r.currentXrayVersionText()
				if version != "synthetic-a" && version != "synthetic-b" {
					t.Errorf("unexpected version snapshot: %q", version)
					return
				}
			}
		}()
	}
	close(start)
	workers.Wait()
}

func persistenceFixture(t *testing.T, pending bool) *store.Store {
	t.Helper()
	s := deletionSafetyFixture(t)
	server := s.Data.Servers["srv_b"]
	server.Status, server.XrayVersion, server.ConfigHash = "online", "synthetic-xray", "synthetic-config"
	server.LastSyncMessage = "synthetic-sync"
	server.CPUUsage, server.MemoryUsage, server.DiskUsage = 5, 25, 30
	server.UploadTotal, server.DownloadTotal = 101, 202
	server.BBRStatus = model.BBRStatus{Supported: true, Enabled: true, CongestionControl: "bbr", DefaultQdisc: "fq", AvailableCongestionControl: []string{"cubic", "bbr"}}
	if pending {
		server.BBRPendingAction = &model.AgentSystemAction{ID: "synthetic-pending", Action: "enable-bbr"}
	}
	s.Data.Servers["srv_b"] = server
	persistDeletionFixture(t, s)
	return s
}

func persistenceSnapshot(t *testing.T, data model.PanelData) model.PanelData {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot model.PanelData
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func persistenceRequest(t *testing.T, method, path, payload string, agent bool) *http.Request {
	t.Helper()
	request := authenticatedSafetyRequest(t, method, path)
	request.Body = io.NopCloser(strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	if agent {
		request.Header.Set("X-Agent-Token", "synthetic-token-srv_b")
	}
	return request
}

func assertPersistenceUnchanged(t *testing.T, s *store.Store, before model.PanelData, path string, file []byte, modified time.Time) {
	t.Helper()
	if !reflect.DeepEqual(before, s.Data) {
		t.Fatal("rejected write changed official memory, tasks or logs")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(file, after) || !info.ModTime().Equal(modified) {
		t.Fatal("rejected write changed database bytes or modification time")
	}
}

func TestServerWriteFailuresDoNotCommitOrReturnSuccess(t *testing.T) {
	cases := []struct {
		name, method, path, payload string
		agent                       bool
	}{
		{"create", "POST", "/api/servers", `{"name":"synthetic-new"}`, false},
		{"update", "PUT", "/api/servers/srv_b", `{"name":"after"}`, false},
		{"delete", "DELETE", "/api/servers/srv_b", "", false},
		{"heartbeat", "POST", "/api/agent/heartbeat", `{"server_id":"srv_b","agent_version":"synthetic-next","completed_action_id":"synthetic-pending","completed_action_result":"done","bbr_status":{"enabled":false}}`, true},
		{"sync", "POST", "/api/agent/sync", `{"server_id":"srv_b","agent_version":"synthetic-next","config_hash":"synthetic-hash"}`, true},
		{"bbr", "POST", "/api/system/optimization/bbr/enable", `{"server_id":"srv_b"}`, false},
	}
	for _, operation := range cases {
		for _, mode := range []string{"serialization", "create", "rename"} {
			t.Run(operation.name+"/"+mode, func(t *testing.T) {
				s := persistenceFixture(t, operation.name == "heartbeat")
				before := persistenceSnapshot(t, s.Data)
				path := s.Path
				file, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
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
				response := httptest.NewRecorder()
				NewRouter(s).ServeHTTP(response, persistenceRequest(t, operation.method, operation.path, operation.payload, operation.agent))
				if response.Code != http.StatusInternalServerError {
					t.Fatalf("write failure returned HTTP %d, expected 500", response.Code)
				}
				var result struct {
					Error string `json:"error"`
					OK    bool   `json:"ok"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.OK || result.Error == "" {
					t.Fatal("failure returned success or omitted error")
				}
				if bytes.Contains(response.Body.Bytes(), []byte(filepath.Dir(path))) || bytes.Contains(response.Body.Bytes(), []byte("synthetic-token-")) {
					t.Fatal("error response leaked paths or credentials")
				}
				assertPersistenceUnchanged(t, s, before, path, file, info.ModTime())
			})
		}
	}
}

func TestServerPUTPreservesProtectedFields(t *testing.T) {
	cases := []struct {
		name, payload string
		status        int
	}{
		{"partial", `{"name":"after"}`, 200},
		{"empty_name", `{"name":""}`, 200},
		{"unknown", `{"unknown_note":"ignored"}`, 200},
		{"full_round_trip", "", 200},
		{"null_body", `null`, 400},
		{"null_name", `{"name":null}`, 400},
		{"wrong_type", `{"name":7}`, 400},
		{"id", `{"id":"other"}`, 409},
		{"created_at", `{"created_at":"2020-01-01T00:00:00Z"}`, 409},
		{"token", `{"agent_token":""}`, 409},
		{"status", `{"status":"offline"}`, 409},
		{"agent_version", `{"agent_version":""}`, 409},
		{"xray_version", `{"xray_version":""}`, 409},
		{"config_hash", `{"config_hash":""}`, 409},
		{"sync_time", `{"last_sync_at":"0001-01-01T00:00:00Z"}`, 409},
		{"sync_message", `{"last_sync_message":""}`, 409},
		{"cpu", `{"cpu_usage":0}`, 409},
		{"memory", `{"memory_usage":0}`, 409},
		{"disk", `{"disk_usage":0}`, 409},
		{"upload", `{"upload_total":0}`, 409},
		{"download", `{"download_total":0}`, 409},
		{"bbr_status", `{"bbr_status":{}}`, 409},
		{"clear_pending", `{"bbr_pending_action":null}`, 409},
		{"replace_pending", `{"bbr_pending_action":{"id":"other"}}`, 409},
		{"null_token", `{"agent_token":null}`, 400},
		{"ignore_updated_at", `{"updated_at":"2020-01-01T00:00:00Z"}`, 200},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			s := persistenceFixture(t, true)
			before := persistenceSnapshot(t, s.Data)
			file, modified := persistDeletionFixture(t, s)
			payload := test.payload
			if test.name == "full_round_trip" {
				server := s.Data.Servers["srv_b"]
				server.Name = "after"
				raw, err := json.Marshal(server)
				if err != nil {
					t.Fatal(err)
				}
				payload = string(raw)
			}
			response := httptest.NewRecorder()
			NewRouter(s).ServeHTTP(response, persistenceRequest(t, "PUT", "/api/servers/srv_b", payload, false))
			if response.Code != test.status {
				t.Fatalf("HTTP %d, expected %d (%s)", response.Code, test.status, response.Body.String())
			}
			if test.status != 200 {
				assertPersistenceUnchanged(t, s, before, s.Path, file, modified)
				return
			}
			expected := before.Servers["srv_b"]
			if test.name == "partial" || test.name == "full_round_trip" {
				expected.Name = "after"
			}
			if test.name == "empty_name" {
				expected.Name = ""
			}
			after := s.Data.Servers["srv_b"]
			if after.UpdatedAt.Equal(expected.UpdatedAt) {
				t.Fatal("server must own the update timestamp")
			}
			after.UpdatedAt = expected.UpdatedAt
			if !reflect.DeepEqual(expected, after) {
				t.Fatal("metadata PUT changed protected or omitted fields")
			}
			var returned model.Server
			if err := json.Unmarshal(response.Body.Bytes(), &returned); err != nil {
				t.Fatal(err)
			}
			if returned.BBRPendingAction == nil || returned.BBRPendingAction.ID != "synthetic-pending" || returned.AgentToken != expected.AgentToken {
				t.Fatal("success response lost protected fields")
			}
		})
	}
}

func TestServerPUTMergesAllEditableFields(t *testing.T) {
	s := persistenceFixture(t, true)
	before := persistenceSnapshot(t, s.Data).Servers["srv_b"]
	response := httptest.NewRecorder()
	NewRouter(s).ServeHTTP(response, persistenceRequest(t, "PUT", "/api/servers/srv_b", `{"name":"after","ip":"192.0.2.20","host":"synthetic.invalid","region":"TEST","provider":"Synthetic"}`, false))
	if response.Code != 200 {
		t.Fatalf("HTTP %d", response.Code)
	}
	after := s.Data.Servers["srv_b"]
	if after.Name != "after" || after.IP != "192.0.2.20" || after.Host != "synthetic.invalid" || after.Region != "TEST" || after.Provider != "Synthetic" {
		t.Fatal("editable fields not merged")
	}
	after.Name, after.IP, after.Host, after.Region, after.Provider = before.Name, before.IP, before.Host, before.Region, before.Provider
	after.UpdatedAt = before.UpdatedAt
	if !reflect.DeepEqual(before, after) {
		t.Fatal("editable merge changed protected fields")
	}
}

func TestServerIdentityMismatchCannotWriteAnotherServer(t *testing.T) {
	cases := []struct {
		method, path, payload string
		agent                 bool
	}{
		{"PUT", "/api/servers/srv_b", `{"name":"after"}`, false},
		{"POST", "/api/agent/heartbeat", `{"server_id":"srv_b","agent_version":"after"}`, true},
		{"POST", "/api/agent/sync", `{"server_id":"srv_b","agent_version":"after"}`, true},
		{"POST", "/api/system/optimization/bbr/enable", `{"server_id":"srv_b"}`, false},
	}
	for _, test := range cases {
		t.Run(test.path, func(t *testing.T) {
			s := persistenceFixture(t, false)
			server := s.Data.Servers["srv_b"]
			server.ID = "srv_a"
			s.Data.Servers["srv_b"] = server
			before := persistenceSnapshot(t, s.Data)
			file, modified := persistDeletionFixture(t, s)
			response := httptest.NewRecorder()
			NewRouter(s).ServeHTTP(response, persistenceRequest(t, test.method, test.path, test.payload, test.agent))
			if response.Code != 409 {
				t.Fatalf("inconsistent identity returned HTTP %d", response.Code)
			}
			assertPersistenceUnchanged(t, s, before, s.Path, file, modified)
		})
	}
}

func TestBBRPendingActionCannotBeOverwritten(t *testing.T) {
	for _, path := range []string{"/api/system/optimization/bbr/status", "/api/system/optimization/bbr/enable", "/api/system/optimization/bbr/disable"} {
		for _, emptyID := range []bool{false, true} {
			t.Run(path+"/"+map[bool]string{false: "normal", true: "empty_id"}[emptyID], func(t *testing.T) {
				s := persistenceFixture(t, true)
				if emptyID {
					s.Data.Servers["srv_b"].BBRPendingAction.ID = ""
				}
				before := persistenceSnapshot(t, s.Data)
				file, modified := persistDeletionFixture(t, s)
				response := httptest.NewRecorder()
				NewRouter(s).ServeHTTP(response, persistenceRequest(t, "POST", path, `{"server_id":"srv_b"}`, false))
				if response.Code != 409 {
					t.Fatalf("pending action returned HTTP %d", response.Code)
				}
				assertPersistenceUnchanged(t, s, before, s.Path, file, modified)
			})
		}
	}
}

func TestAgentTaskCompletionUsesCurrentIDAndCommitsTogether(t *testing.T) {
	for _, id := range []string{"synthetic-pending", "stale-id"} {
		t.Run(id, func(t *testing.T) {
			s := persistenceFixture(t, true)
			response := httptest.NewRecorder()
			payload := `{"server_id":"srv_b","agent_version":"synthetic-next","completed_action_id":"` + id + `","completed_action_result":"done"}`
			NewRouter(s).ServeHTTP(response, persistenceRequest(t, "POST", "/api/agent/heartbeat", payload, true))
			if response.Code != 200 {
				t.Fatalf("HTTP %d", response.Code)
			}
			pending := s.Data.Servers["srv_b"].BBRPendingAction
			if id == "synthetic-pending" {
				if pending != nil || len(s.Data.OperationLogs) != 1 {
					t.Fatal("completion and operation log must commit together")
				}
			} else if pending == nil || pending.ID != "synthetic-pending" || len(s.Data.OperationLogs) != 0 {
				t.Fatal("stale completion cleared current task")
			}
			file, err := os.ReadFile(s.Path)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := json.MarshalIndent(s.Data, "", "  ")
			if err != nil || !bytes.Equal(file, expected) {
				t.Fatal("heartbeat file differs from committed memory")
			}
		})
	}
}

func TestAgentSyncSuccessKeepsProtocolAndGeneratedConfig(t *testing.T) {
	s := persistenceFixture(t, true)
	expectedConfig := xray.GenerateServerConfig(nil, nil, nil, s.Data.Nodes, s.Data.NetworkPolicy)
	hash := xray.ConfigHash(expectedConfig)
	response := httptest.NewRecorder()
	payload := `{"server_id":"srv_b","agent_version":"synthetic-next","config_hash":"` + hash + `"}`
	NewRouter(s).ServeHTTP(response, persistenceRequest(t, "POST", "/api/agent/sync", payload, true))
	if response.Code != 200 {
		t.Fatalf("HTTP %d", response.Code)
	}
	var result model.AgentSyncResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	actual, err := json.Marshal(result.XrayConfig)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := json.Marshal(expectedConfig)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.ServerID != "srv_b" || result.DesiredConfigHash != hash || result.RestartRequired || result.NextIntervalSeconds != 30 || result.SystemAction == nil || result.SystemAction.ID != "synthetic-pending" || !bytes.Equal(expected, actual) {
		t.Fatal("sync success contract or generated config changed")
	}
}

func TestServerReadEndpointsDoNotSave(t *testing.T) {
	for _, path := range []string{"/api/servers/srv_b", "/api/updates/status", "/api/updates/xray-status"} {
		t.Run(path, func(t *testing.T) {
			s := persistenceFixture(t, true)
			before := persistenceSnapshot(t, s.Data)
			file, modified := persistDeletionFixture(t, s)
			response := httptest.NewRecorder()
			NewRouter(s).ServeHTTP(response, authenticatedSafetyRequest(t, "GET", path))
			if response.Code != 200 {
				t.Fatalf("HTTP %d", response.Code)
			}
			assertPersistenceUnchanged(t, s, before, s.Path, file, modified)
		})
	}
}

func TestServerCreateAndBBRQueueCommitSuccess(t *testing.T) {
	for _, path := range []string{"/api/servers", "/api/system/optimization/bbr/enable", "/api/system/optimization/bbr/disable", "/api/system/optimization/bbr/status"} {
		t.Run(path, func(t *testing.T) {
			s := persistenceFixture(t, false)
			payload := `{"server_id":"srv_b"}`
			status := http.StatusAccepted
			if path == "/api/servers" {
				payload, status = `{"name":"synthetic-new"}`, http.StatusCreated
			}
			response := httptest.NewRecorder()
			NewRouter(s).ServeHTTP(response, persistenceRequest(t, "POST", path, payload, false))
			if response.Code != status {
				t.Fatalf("HTTP %d, expected %d", response.Code, status)
			}
			if path == "/api/servers" {
				var server model.Server
				if err := json.Unmarshal(response.Body.Bytes(), &server); err != nil {
					t.Fatal(err)
				}
				if server.ID == "" || server.AgentToken == "" || server.Status != "offline" || server.Name != "synthetic-new" || len(s.Data.Servers) != 3 || s.Data.Servers[server.ID].ID != server.ID {
					t.Fatal("server creation contract changed")
				}
			} else {
				pending := s.Data.Servers["srv_b"].BBRPendingAction
				want := map[string]string{"/api/system/optimization/bbr/enable": "enable-bbr", "/api/system/optimization/bbr/disable": "disable-bbr", "/api/system/optimization/bbr/status": "bbr-status"}[path]
				if pending == nil || pending.ID == "" || pending.Action != want {
					t.Fatal("BBR task not committed")
				}
			}
			if len(s.Data.OperationLogs) != 1 {
				t.Fatal("successful write must commit one operation log")
			}
			actual, err := os.ReadFile(s.Path)
			if err != nil {
				t.Fatal(err)
			}
			expected, err := json.MarshalIndent(s.Data, "", "  ")
			if err != nil || !bytes.Equal(expected, actual) {
				t.Fatal("successful memory and disk state differ")
			}
		})
	}
}

func TestServerMetadataAndHeartbeatConcurrentUpdatesPreserveBoth(t *testing.T) {
	for round := 0; round < 20; round++ {
		s := persistenceFixture(t, true)
		handler := NewRouter(s)
		requests := []*http.Request{
			persistenceRequest(t, "PUT", "/api/servers/srv_b", `{"name":"after"}`, false),
			persistenceRequest(t, "POST", "/api/agent/heartbeat", `{"server_id":"srv_b","agent_version":"synthetic-heartbeat"}`, true),
		}
		responses := []*httptest.ResponseRecorder{httptest.NewRecorder(), httptest.NewRecorder()}
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
		for _, response := range responses {
			if response.Code != 200 {
				t.Fatalf("round %d: HTTP %d", round, response.Code)
			}
		}
		server := s.Data.Servers["srv_b"]
		if server.Name != "after" || server.AgentVersion != "synthetic-heartbeat" || server.BBRPendingAction == nil || server.BBRPendingAction.ID != "synthetic-pending" || len(s.Data.Servers) != 2 {
			t.Fatalf("round %d: metadata, telemetry or task lost", round)
		}
		file, err := os.ReadFile(s.Path)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := json.MarshalIndent(s.Data, "", "  ")
		if err != nil || !bytes.Equal(file, expected) {
			t.Fatalf("round %d: committed file differs", round)
		}
	}
}
