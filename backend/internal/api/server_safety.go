// SPDX-License-Identifier: AGPL-3.0-only
package api

import (
	"os"
	"sort"
	"strings"
)

type serverDeleteConflict struct {
	Kind       string `json:"kind"`
	ResourceID string `json:"resource_id,omitempty"`
	Message    string `json:"message"`
}

// Caller must hold the store write lock through the check and deletion.
func (r *Router) serverDeleteConflictsLocked(id string) []serverDeleteConflict {
	conflicts := make([]serverDeleteConflict, 0)
	if len(r.store.Data.Servers) <= 1 {
		conflicts = append(conflicts, serverDeleteConflict{
			Kind: "last_server_protected", ResourceID: id,
			Message: "The last server cannot be deleted.",
		})
	}

	localID := strings.TrimSpace(os.Getenv("ZXY_LOCAL_SERVER_ID"))
	local, exists := r.store.Data.Servers[localID]
	switch {
	case localID == "":
		conflicts = append(conflicts, serverDeleteConflict{
			Kind:    "local_identity_unknown",
			Message: "Local server identity is unknown; explicitly configure ZXY_LOCAL_SERVER_ID in the API environment before deleting a remote server.",
		})
	case !exists || local.ID != localID:
		conflicts = append(conflicts, serverDeleteConflict{
			Kind:    "local_identity_invalid",
			Message: "ZXY_LOCAL_SERVER_ID does not identify a valid local server record; verify the local binding before deleting.",
		})
	case localID == id:
		conflicts = append(conflicts, serverDeleteConflict{
			Kind: "local_server_protected", ResourceID: id,
			Message: "The explicitly configured local server cannot be deleted.",
		})
	}

	nodeIDs := make(map[string]bool)
	for nodeID, node := range r.store.Data.Nodes {
		if node.ServerID == id {
			nodeIDs[nodeID] = true
			conflicts = append(conflicts, serverDeleteConflict{
				Kind: "node_reference", ResourceID: nodeID,
				Message: "Nodes still reference this server, including disabled nodes; resolve those references before deleting.",
			})
		}
	}

	relayIDs := make(map[string]bool)
	for relayID, relay := range r.store.Data.RelayRoutes {
		if relay.RelayServerID == id || nodeIDs[relay.LandingNodeID] {
			relayIDs[relayID] = true
			conflicts = append(conflicts, serverDeleteConflict{
				Kind: "relay_reference", ResourceID: relayID,
				Message: "Relay routes still reference this server or its landing nodes, including disabled routes; resolve those references before deleting.",
			})
		}
	}

	for clientID, client := range r.store.Data.Clients {
		referenced := false
		for _, nodeID := range client.NodeIDs {
			if nodeIDs[nodeID] {
				referenced = true
				break
			}
		}
		if !referenced {
			for _, relayID := range client.RelayRouteIDs {
				if relayIDs[relayID] {
					referenced = true
					break
				}
			}
		}
		if referenced {
			conflicts = append(conflicts, serverDeleteConflict{
				Kind: "client_reference", ResourceID: clientID,
				Message: "Clients still indirectly reference this server through nodes or relay routes, including disabled clients; resolve their bindings before deleting.",
			})
		}
	}

	if action := r.store.Data.Servers[id].BBRPendingAction; action != nil {
		conflicts = append(conflicts, serverDeleteConflict{
			Kind: "bbr_pending_action", ResourceID: action.ID,
			Message: "A BBR action is pending for this server; confirm completion or explicitly cancel it before deleting.",
		})
	}

	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Kind != conflicts[j].Kind {
			return conflicts[i].Kind < conflicts[j].Kind
		}
		return conflicts[i].ResourceID < conflicts[j].ResourceID
	})
	return conflicts
}
