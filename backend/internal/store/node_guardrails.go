// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"errors"
	"sort"

	"zxy-panel/backend/internal/model"
)

var ErrNodeNotFound = errors.New("node not found")

type NodeConflict struct {
	Kind       string `json:"kind"`
	ResourceID string `json:"resource_id,omitempty"`
	Message    string `json:"message"`
}

type NodeConflictError struct {
	Conflicts []NodeConflict
}

func (e *NodeConflictError) Error() string {
	return "Node identity or references conflict with this operation; no changes committed."
}

func (s *Store) nodeLocked(id string) (model.Node, error) {
	node, exists := s.Data.Nodes[id]
	if id == "" || !exists {
		return model.Node{}, ErrNodeNotFound
	}
	if node.ID != id {
		return model.Node{}, &NodeConflictError{Conflicts: []NodeConflict{{
			Kind: "node_identity_inconsistent", ResourceID: id,
			Message: "The node record does not match its map key; no identity repair was performed.",
		}}}
	}
	return node, nil
}

func (s *Store) nodeReferencesLocked(id string) []NodeConflict {
	conflicts := make([]NodeConflict, 0)
	for clientID, client := range s.Data.Clients {
		for _, nodeID := range client.NodeIDs {
			if nodeID == id {
				conflicts = append(conflicts, NodeConflict{
					Kind: "client_node_reference", ResourceID: clientID,
					Message: "A client still references this node, including disabled or expired clients; explicitly resolve the binding first.",
				})
				break
			}
		}
	}
	for relayID, relay := range s.Data.RelayRoutes {
		if relay.LandingNodeID == id {
			conflicts = append(conflicts, NodeConflict{
				Kind: "relay_landing_reference", ResourceID: relayID,
				Message: "A relay still references this landing node, including disabled or manual-mode records; explicitly resolve the reference first.",
			})
		}
	}
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Kind != conflicts[j].Kind {
			return conflicts[i].Kind < conflicts[j].Kind
		}
		return conflicts[i].ResourceID < conflicts[j].ResourceID
	})
	return conflicts
}

func (s *Store) validateNodeServerLocked(id, serverID string) error {
	server, exists := s.Data.Servers[serverID]
	if serverID == "" || !exists || server.ID != serverID {
		return &NodeConflictError{Conflicts: []NodeConflict{{
			Kind: "node_server_invalid", ResourceID: id,
			Message: "The node server is missing or inconsistent; no server was selected or repaired automatically.",
		}}}
	}
	return nil
}

// Caller must hold Mu.Lock through validation and commit.
func (s *Store) DeleteNodeLocked(id, actor, ip string) error {
	if _, err := s.nodeLocked(id); err != nil {
		return err
	}
	if conflicts := s.nodeReferencesLocked(id); len(conflicts) > 0 {
		return &NodeConflictError{Conflicts: conflicts}
	}
	next := clonePanelData(s.Data)
	delete(next.Nodes, id)
	addLog(&next, actor, "node.delete", ip, id)
	return s.commitLocked(next)
}

// Caller must hold Mu.Lock through request validation and commit.
func (s *Store) UpdateNodeLocked(id string, node model.Node, actor, ip string) error {
	current, err := s.nodeLocked(id)
	if err != nil {
		return err
	}
	if node.ID != id {
		return &NodeConflictError{Conflicts: []NodeConflict{{
			Kind: "node_identity_inconsistent", ResourceID: id,
			Message: "The proposed node ID does not match the update target.",
		}}}
	}
	if node.ServerID != current.ServerID {
		if conflicts := s.nodeReferencesLocked(id); len(conflicts) > 0 {
			return &NodeConflictError{Conflicts: conflicts}
		}
	}
	if err := s.validateNodeServerLocked(id, current.ServerID); err != nil {
		return err
	}
	if err := s.validateNodeServerLocked(id, node.ServerID); err != nil {
		return err
	}
	node.CreatedAt = current.CreatedAt
	next := clonePanelData(s.Data)
	next.Nodes[id] = node
	addLog(&next, actor, "node.update", ip, id)
	return s.commitLocked(next)
}
