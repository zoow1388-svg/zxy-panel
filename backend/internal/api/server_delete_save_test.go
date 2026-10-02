// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"zxy-panel/backend/internal/model"
)

func TestServerDeleteSaveFailureRestoresState(t *testing.T) {
	for _, mode := range []string{"write_failure", "rename_failure"} {
		t.Run(mode, func(t *testing.T) {
			s := deletionSafetyFixture(t)
			s.Data.Version = "synthetic-before-save"
			s.Data.Admins["admin_synthetic"] = model.AdminUser{ID: "admin_synthetic", Username: "synthetic-admin", PasswordHash: ""}
			before, modified := persistDeletionFixture(t, s)
			originalPath := s.Path
			if mode == "write_failure" {
				if err := os.Mkdir(s.Path+".tmp", 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				s.Path = filepath.Join(filepath.Dir(s.Path), "nonempty-target")
				if err := os.Mkdir(s.Path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(s.Path, "last-known-good.json"), before, 0600); err != nil {
					t.Fatal(err)
				}
			}
			response := httptest.NewRecorder()
			NewRouter(s).ServeHTTP(response, authenticatedSafetyRequest(t, http.MethodDelete, "/api/servers/srv_b"))
			if response.Code != http.StatusInternalServerError {
				t.Errorf("save failure returned HTTP %d, expected 500", response.Code)
			}
			var result struct {
				Error string `json:"error"`
				Code  string `json:"code"`
				OK    bool   `json:"ok"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.OK || result.Error == "" || result.Code != "server_delete_save_failed" {
				t.Error("save failure must be explicit and must not return success")
			}
			after, err := json.MarshalIndent(s.Data, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Error("failed deletion changed Server identity, logs, admin normalization or metadata")
			}
			file, err := os.ReadFile(originalPath)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(originalPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, file) || !info.ModTime().Equal(modified) {
				t.Error("failed deletion changed the previous persisted database")
			}
			if bytes.Contains(response.Body.Bytes(), []byte(filepath.Dir(originalPath))) || bytes.Contains(response.Body.Bytes(), []byte("synthetic-token-")) {
				t.Error("save error response leaked filesystem paths or credentials")
			}
		})
	}
}

func TestServerDeleteSerializationFailureRestoresState(t *testing.T) {
	s := deletionSafetyFixture(t)
	s.Data.Version = "synthetic-before-save"
	s.Data.Admins["admin_synthetic"] = model.AdminUser{ID: "admin_synthetic", Username: "synthetic-admin"}
	beforeFile, modified := persistDeletionFixture(t, s)
	server := s.Data.Servers["srv_a"]
	server.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	s.Data.Servers["srv_a"] = server
	beforeState := s.Data
	beforeState.Servers = maps.Clone(s.Data.Servers)
	beforeState.Admins = maps.Clone(s.Data.Admins)
	beforeState.OperationLogs = maps.Clone(s.Data.OperationLogs)
	if _, err := json.Marshal(s.Data); err == nil {
		t.Fatal("fixture must fail JSON serialization before any filesystem write")
	}
	response := httptest.NewRecorder()
	NewRouter(s).ServeHTTP(response, authenticatedSafetyRequest(t, http.MethodDelete, "/api/servers/srv_b"))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("serialization failure returned HTTP %d, expected 500", response.Code)
	}
	var result struct {
		Code string `json:"code"`
		OK   bool   `json:"ok"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Code != "server_delete_save_failed" {
		t.Error("serialization failure must not return a successful deletion")
	}
	if !reflect.DeepEqual(beforeState, s.Data) {
		t.Error("serialization failure changed in-memory servers, logs or normalized metadata")
	}
	afterFile, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeFile, afterFile) || !info.ModTime().Equal(modified) {
		t.Error("serialization failure changed the last-known-good database")
	}
	if _, err := os.Stat(s.Path + ".tmp"); !os.IsNotExist(err) {
		t.Error("serialization failure must occur before creating a temporary database file")
	}
}
