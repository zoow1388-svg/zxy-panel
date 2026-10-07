// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"zxy-panel/backend/internal/model"
)

func nodeCreateStoreInput() model.Node {
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	return model.Node{ID: "created_node", ServerID: "srv_a", Name: "Synthetic new node",
		Protocol: "socks", Transport: "tcp", Security: "none", Host: "synthetic.invalid", Port: 32101,
		SocksUsername: "synthetic-user", SocksPassword: "synthetic-only", SocksUDP: true,
		Remark: "Synthetic remark", Enabled: true, CreatedAt: now, UpdatedAt: now}
}

func TestNodeCreateStoreRejectsEmptyAndDuplicateID(t *testing.T) {
	for _, id := range []string{"", " \t\n", "node"} {
		t.Run("id_"+strings.ReplaceAll(id, "\n", "newline"), func(t *testing.T) {
			s := commitFixture(t)
			before := clonePanelData(s.Data)
			raw, err := os.ReadFile(s.Path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(s.Path)
			if err != nil {
				t.Fatal(err)
			}
			node := nodeCreateStoreInput()
			node.ID = id
			s.Mu.Lock()
			err = s.CreateNodeLocked(node, "synthetic", "synthetic-ip")
			s.Mu.Unlock()
			var conflict *NodeConflictError
			if !errors.As(err, &conflict) || len(conflict.Conflicts) != 1 || conflict.Conflicts[0].Kind != "node_identity_inconsistent" {
				t.Fatal("empty or duplicate ID must return identity conflict")
			}
			if !reflect.DeepEqual(before, s.Data) {
				t.Fatal("rejected create changed full official memory")
			}
			assertCommitFileUnchanged(t, s.Path, raw, info.ModTime())
		})
	}
}

func TestNodeCreateStoreCommitsNodeAndLogTogether(t *testing.T) {
	s := commitFixture(t)
	before := clonePanelData(s.Data)
	normalize(&before)
	node := nodeCreateStoreInput()
	s.Mu.Lock()
	err := s.CreateNodeLocked(node, "synthetic", "synthetic-ip")
	s.Mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	stored := s.Data.Nodes[node.ID]
	if !stored.CreatedAt.Equal(node.CreatedAt) || !stored.UpdatedAt.Equal(node.UpdatedAt) {
		t.Fatal("node times changed")
	}
	stored.CreatedAt, stored.UpdatedAt = time.Time{}, time.Time{}
	wantNode := node
	wantNode.CreatedAt, wantNode.UpdatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(stored, wantNode) {
		t.Fatal("created node fields changed")
	}
	if len(s.Data.Nodes) != len(before.Nodes)+1 || len(s.Data.OperationLogs) != len(before.OperationLogs)+1 {
		t.Fatal("create must commit exactly one node and log")
	}
	after := clonePanelData(s.Data)
	delete(after.Nodes, node.ID)
	for id, entry := range after.OperationLogs {
		if _, exists := before.OperationLogs[id]; exists {
			continue
		}
		if entry.ID != id || entry.Actor != "synthetic" || entry.Action != "node.create" || entry.IP != "synthetic-ip" || entry.Detail != node.Name || entry.CreatedAt.IsZero() {
			t.Fatal("creation log differs from existing business output")
		}
		delete(after.OperationLogs, id)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("create changed unrelated complete data, bindings or BBR")
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.MarshalIndent(s.Data, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, want) {
		t.Fatal("disk and memory differ after create")
	}
	node.Name = "caller changed"
	if s.Data.Nodes[node.ID].Name == node.Name {
		t.Fatal("created node aliases caller input")
	}
}

func TestNodeCreateStoreFailurePreservesMemoryAndDisk(t *testing.T) {
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
			node := nodeCreateStoreInput()
			var keep string
			var keepTime time.Time
			switch mode {
			case "serialization":
				node.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			case "create":
				s.Path = filepath.Join(filepath.Dir(path), "missing-parent", "panel.json")
			case "rename":
				s.Path = filepath.Join(filepath.Dir(path), "nonempty-target")
				if err := os.Mkdir(s.Path, 0700); err != nil {
					t.Fatal(err)
				}
				keep = filepath.Join(s.Path, "keep")
				if err := os.WriteFile(keep, raw, 0600); err != nil {
					t.Fatal(err)
				}
				keepInfo, err := os.Stat(keep)
				if err != nil {
					t.Fatal(err)
				}
				keepTime = keepInfo.ModTime()
			}
			faultPath := s.Path
			s.Mu.Lock()
			err = s.CreateNodeLocked(node, "synthetic", "synthetic-ip")
			s.Mu.Unlock()
			if err == nil {
				t.Fatal("save fault must return failure")
			}
			if !reflect.DeepEqual(before, s.Data) {
				t.Fatal("failed create changed full memory, bindings, BBR or logs")
			}
			if s.Path != faultPath {
				t.Fatal("failed create changed store path")
			}
			if _, exists := s.Data.Nodes[node.ID]; exists {
				t.Fatal("failed create left a node")
			}
			assertCommitFileUnchanged(t, path, raw, info.ModTime())
			if keep != "" {
				assertCommitFileUnchanged(t, keep, raw, keepTime)
			}
		})
	}
}
