// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func callsStoreLock(call *ast.CallExpr, name string) bool {
	method, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || method.Sel.Name != name {
		return false
	}
	mutex, ok := method.X.(*ast.SelectorExpr)
	if !ok || mutex.Sel.Name != "Mu" {
		return false
	}
	store, ok := mutex.X.(*ast.SelectorExpr)
	if !ok || store.Sel.Name != "store" {
		return false
	}
	receiver, ok := store.X.(*ast.Ident)
	return ok && receiver.Name == "r"
}

func TestAgentAuthorizationSharesWriteLock(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "agent.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"agentHeartbeat", "agentSync"} {
		t.Run(name, func(t *testing.T) {
			lock, unlock, auth := -1, -1, -1
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Name.Name != name {
					continue
				}
				for index, statement := range function.Body.List {
					switch statement := statement.(type) {
					case *ast.ExprStmt:
						if call, ok := statement.X.(*ast.CallExpr); ok {
							if callsStoreLock(call, "Lock") {
								lock = index
							}
						}
					case *ast.DeferStmt:
						if callsStoreLock(statement.Call, "Unlock") {
							unlock = index
						}
					case *ast.IfStmt:
						condition, ok := statement.Cond.(*ast.UnaryExpr)
						if !ok || condition.Op != token.NOT {
							continue
						}
						call, ok := condition.X.(*ast.CallExpr)
						if !ok {
							continue
						}
						if selector, ok := call.Fun.(*ast.SelectorExpr); ok && strings.HasPrefix(selector.Sel.Name, "validateAgentToken") {
							auth = index
						}
					}
				}
			}
			if lock < 0 || unlock <= lock || auth <= unlock {
				t.Error("Agent authorization must run inside the same deferred write lock as mutation")
			}
		})
	}
}

func syntheticAgentRequest(path string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"server_id":"srv_b","agent_version":"synthetic-agent","config_hash":"synthetic-hash"}`))
	request.Header.Set("X-Agent-Token", "synthetic-token-srv_b")
	return request
}

func TestAgentRequestsConcurrentWithServerDelete(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(previous) })
	for _, path := range []string{"/api/agent/heartbeat", "/api/agent/sync"} {
		t.Run(path, func(t *testing.T) {
			for round := 0; round < 40; round++ {
				s := deletionSafetyFixture(t)
				persistDeletionFixture(t, s)
				handler := NewRouter(s)
				start := make(chan struct{})
				codes := make(chan int, 8)
				var requests sync.WaitGroup
				for agent := 0; agent < 8; agent++ {
					requests.Add(1)
					go func() {
						defer requests.Done()
						<-start
						response := httptest.NewRecorder()
						handler.ServeHTTP(response, syntheticAgentRequest(path))
						codes <- response.Code
					}()
				}
				deletion := httptest.NewRecorder()
				deleteRequest := authenticatedSafetyRequest(t, http.MethodDelete, "/api/servers/srv_b")
				requests.Add(1)
				go func() {
					defer requests.Done()
					<-start
					handler.ServeHTTP(deletion, deleteRequest)
				}()
				close(start)
				requests.Wait()
				close(codes)
				if deletion.Code != http.StatusOK {
					t.Fatalf("round %d: DELETE returned HTTP %d", round, deletion.Code)
				}
				for code := range codes {
					if code != http.StatusOK && code != http.StatusNotFound {
						t.Errorf("round %d: Agent returned unexpected HTTP %d", round, code)
					}
				}
				if len(s.Data.Servers) != 1 || s.Data.Servers["srv_a"].ID != "srv_a" {
					t.Fatalf("round %d: deleted Server was recreated or a ghost identity was written", round)
				}
				raw, err := os.ReadFile(s.Path)
				if err != nil {
					t.Fatal(err)
				}
				var persisted struct {
					Servers map[string]struct {
						ID string `json:"id"`
					} `json:"servers"`
				}
				if err := json.Unmarshal(raw, &persisted); err != nil {
					t.Fatal(err)
				}
				if len(persisted.Servers) != 1 || persisted.Servers["srv_a"].ID != "srv_a" {
					t.Fatalf("round %d: deleted Server or ghost identity remained on disk", round)
				}
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, syntheticAgentRequest(path))
				if response.Code != http.StatusNotFound {
					t.Error("deleted Agent identity must remain unregistered")
				}
			}
		})
	}
}
