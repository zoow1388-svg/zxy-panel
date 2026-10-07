// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"zxy-panel/backend/internal/model"
)

func commitFixture(t *testing.T) *Store {
	t.Helper()
	s := &Store{Path: filepath.Join(t.TempDir(), "panel.json"), Data: model.PanelData{
		Version: "synthetic-before",
		Admins:  map[string]model.AdminUser{"admin": {ID: "admin", PasswordHash: "synthetic-hash"}},
		Servers: map[string]model.Server{"srv_a": {ID: "srv_a", Name: "before", AgentToken: "synthetic-only",
			BBRStatus:        model.BBRStatus{AvailableCongestionControl: []string{"cubic", "bbr"}},
			BBRPendingAction: &model.AgentSystemAction{ID: "pending", Action: "bbr-status"}}},
		Nodes:               map[string]model.Node{"node": {ID: "node", ServerID: "srv_a"}},
		Clients:             map[string]model.Client{"client": {ID: "client", NodeIDs: []string{"node"}, RelayRouteIDs: []string{"relay"}}},
		RelayRoutes:         map[string]model.RelayRoute{"relay": {ID: "relay", RelayServerID: "srv_a", LandingNodeID: "node"}},
		LandingExits:        map[string]model.LandingExit{"exit": {ID: "exit"}},
		OperationLogs:       map[string]model.OperationLog{},
		NetworkPolicy:       model.NetworkPolicy{Mode: "custom", DNSServers: []string{"synthetic-dns"}},
		NetworkPolicyBackup: model.NetworkPolicy{Mode: "custom", DNSServers: []string{"synthetic-backup-dns"}},
	}}
	raw, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	modified := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(s.Path, modified, modified); err != nil {
		t.Fatal(err)
	}
	return s
}

func assertCommitFileUnchanged(t *testing.T, path string, raw []byte, modified time.Time) {
	t.Helper()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, after) || !info.ModTime().Equal(modified) {
		t.Fatal("failed commit changed the persisted database or modification time")
	}
}

func TestServerCommitFailureKeepsMemoryAndDisk(t *testing.T) {
	for _, mode := range []string{"serialization", "create", "rename"} {
		t.Run(mode, func(t *testing.T) {
			s := commitFixture(t)
			before := clonePanelData(s.Data)
			path := s.Path
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			server := cloneServer(s.Data.Servers["srv_a"])
			server.Name = "after"
			server.BBRPendingAction = nil
			switch mode {
			case "serialization":
				server.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			case "create":
				s.Path = filepath.Join(filepath.Dir(path), "missing-parent", "panel.json")
			case "rename":
				s.Path = filepath.Join(filepath.Dir(path), "nonempty-target")
				if err := os.Mkdir(s.Path, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(s.Path, "keep"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			s.Mu.Lock()
			err = s.SaveServerLocked("srv_a", server, "synthetic", "server.update", "", "synthetic")
			s.Mu.Unlock()
			if err == nil {
				t.Fatal("fault must fail commit")
			}
			if !reflect.DeepEqual(before, s.Data) {
				t.Fatal("failed commit changed live data or logs")
			}
			assertCommitFileUnchanged(t, path, raw, info.ModTime())
		})
	}
}

func TestSaveLockedDoesNotNormalizeLiveDataOnFailure(t *testing.T) {
	s := commitFixture(t)
	server := s.Data.Servers["srv_a"]
	server.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	s.Data.Servers["srv_a"] = server
	s.Data.Admins["admin"] = model.AdminUser{ID: "admin"}
	before := clonePanelData(s.Data)
	s.Mu.Lock()
	err := s.SaveLocked()
	s.Mu.Unlock()
	if err == nil {
		t.Fatal("invalid timestamp must fail serialization")
	}
	if !reflect.DeepEqual(before, s.Data) {
		t.Fatal("normalization leaked into live data")
	}
	files, err := filepath.Glob(filepath.Join(filepath.Dir(s.Path), ".panel.json.tmp-*"))
	if err != nil || len(files) != 0 {
		t.Fatal("serialization failure created a temporary file")
	}
}

func TestPanelCandidateDeepCopy(t *testing.T) {
	s := commitFixture(t)
	before := clonePanelData(s.Data)
	next := clonePanelData(s.Data)
	delete(next.Admins, "admin")
	delete(next.Nodes, "node")
	delete(next.RelayRoutes, "relay")
	delete(next.LandingExits, "exit")
	next.OperationLogs["new"] = model.OperationLog{ID: "new"}
	server := next.Servers["srv_a"]
	server.BBRStatus.AvailableCongestionControl[0] = "changed"
	server.BBRPendingAction.Action = "changed"
	delete(next.Servers, "srv_a")
	client := next.Clients["client"]
	client.NodeIDs[0], client.RelayRouteIDs[0] = "changed", "changed"
	delete(next.Clients, "client")
	next.NetworkPolicy.DNSServers[0], next.NetworkPolicyBackup.DNSServers[0] = "changed", "changed"
	if !reflect.DeepEqual(before, s.Data) {
		t.Fatal("candidate aliases mutable live data")
	}
}

func TestServerCommitUsesExclusiveStagingAndOwnsInput(t *testing.T) {
	s := commitFixture(t)
	foreign := s.Path + ".tmp"
	if err := os.WriteFile(foreign, []byte("foreign-evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	server := cloneServer(s.Data.Servers["srv_a"])
	server.Name = "after"
	s.Mu.Lock()
	err := s.SaveServerLocked("srv_a", server, "synthetic", "server.update", "", "synthetic")
	s.Mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, expected) || len(s.Data.OperationLogs) != 1 {
		t.Fatal("memory, file and log commit differ")
	}
	server.BBRStatus.AvailableCongestionControl[0] = "changed"
	server.BBRPendingAction.Action = "changed"
	if s.Data.Servers["srv_a"].BBRPendingAction.Action != "bbr-status" || s.Data.Servers["srv_a"].BBRStatus.AvailableCongestionControl[0] != "cubic" {
		t.Fatal("committed server aliases caller-owned input")
	}
	foreignRaw, err := os.ReadFile(foreign)
	if err != nil || string(foreignRaw) != "foreign-evidence" {
		t.Fatal("foreign staging evidence overwritten")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(s.Path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("database permissions must be 0600")
		}
	}
}

type faultPanelFile struct {
	mode string
	bytes.Buffer
	closed  int
	synced  bool
	failure error
}

func (f *faultPanelFile) Write(raw []byte) (int, error) {
	if f.mode == "write" {
		return 0, f.failure
	}
	if f.mode == "short_write" {
		return f.Buffer.Write(raw[:len(raw)/2])
	}
	return f.Buffer.Write(raw)
}
func (f *faultPanelFile) Sync() error {
	f.synced = true
	if f.mode == "sync" {
		return f.failure
	}
	return nil
}
func (f *faultPanelFile) Close() error {
	f.closed++
	if f.mode == "close" {
		return f.failure
	}
	return nil
}
func (f *faultPanelFile) Name() string { return "synthetic-staging" }

type faultPanelFiles struct {
	file    *faultPanelFile
	target  string
	renamed bool
}

func (f *faultPanelFiles) CreateTemp(string, string) (stagedPanelFile, error) {
	if f.file.mode == "create" {
		return nil, f.file.failure
	}
	return f.file, nil
}
func (f *faultPanelFiles) Rename(string, string) error {
	if f.file.mode == "rename" {
		return f.file.failure
	}
	f.renamed = true
	f.target = f.file.String()
	return nil
}

func TestPanelFileCommitChecksEveryIOError(t *testing.T) {
	for _, mode := range []string{"create", "write", "short_write", "sync", "close", "rename", "success"} {
		t.Run(mode, func(t *testing.T) {
			failure := errors.New("synthetic I/O failure")
			file := &faultPanelFile{mode: mode, failure: failure}
			files := &faultPanelFiles{file: file, target: "before"}
			err := writePanelData("synthetic-panel.json", []byte("after"), files)
			if mode == "success" {
				if err != nil || files.target != "after" || !files.renamed || !file.synced || file.closed != 1 {
					t.Fatal("incomplete successful commit")
				}
				return
			}
			want := failure
			if mode == "short_write" {
				want = io.ErrShortWrite
			}
			if !errors.Is(err, want) || files.target != "before" || files.renamed {
				t.Fatal("I/O failure ignored or destination replaced")
			}
			if mode != "create" && file.closed != 1 {
				t.Fatal("temporary file must be closed exactly once")
			}
		})
	}
}

func TestServerCommitRejectsInconsistentIdentity(t *testing.T) {
	s := commitFixture(t)
	before := clonePanelData(s.Data)
	s.Mu.Lock()
	err := s.SaveServerLocked("srv_b", s.Data.Servers["srv_a"], "", "", "", "")
	s.Mu.Unlock()
	if err == nil || !reflect.DeepEqual(before, s.Data) {
		t.Fatal("inconsistent target was committed")
	}
}
