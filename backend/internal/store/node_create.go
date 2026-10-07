// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"strings"

	"zxy-panel/backend/internal/model"
)

// Caller must hold Mu.Lock through request validation and commit.
func (s *Store) CreateNodeLocked(node model.Node, actor, ip string) error {
	if strings.TrimSpace(node.ID) == "" {
		return &NodeConflictError{Conflicts: []NodeConflict{{
			Kind:    "node_identity_inconsistent",
			Message: "A new node requires a non-empty ID.",
		}}}
	}
	if _, exists := s.Data.Nodes[node.ID]; exists {
		return &NodeConflictError{Conflicts: []NodeConflict{{
			Kind: "node_identity_inconsistent", ResourceID: node.ID,
			Message: "The node ID is already in use.",
		}}}
	}
	next := clonePanelData(s.Data)
	if next.Nodes == nil {
		next.Nodes = make(map[string]model.Node)
	}
	next.Nodes[node.ID] = node
	addLog(&next, actor, "node.create", ip, node.Name)
	return s.commitLocked(next)
}
