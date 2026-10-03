// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"errors"
	"sort"
	"strings"

	"zxy-panel/backend/internal/model"
)

var (
	ErrRelayNotFound        = errors.New("relay not found")
	ErrClientNotFound       = errors.New("client not found")
	ErrBindingTargetMissing = errors.New("binding target does not exist")
)

type BindingConflict struct {
	Kind       string `json:"kind"`
	ResourceID string `json:"resource_id,omitempty"`
	Message    string `json:"message"`
}

type BindingConflictError struct{ Conflicts []BindingConflict }

func (e *BindingConflictError) Error() string {
	return "Identity, ownership or references conflict with this operation; no changes committed."
}

func bindingConflict(kind, id, message string) error {
	return &BindingConflictError{Conflicts: []BindingConflict{{Kind: kind, ResourceID: id, Message: message}}}
}

// Caller holds Mu.Lock until the mutation has committed.
func (s *Store) RelayLocked(id string) (model.RelayRoute, error) {
	relay, ok := s.Data.RelayRoutes[id]
	if id == "" || !ok {
		return model.RelayRoute{}, ErrRelayNotFound
	}
	if relay.ID != id {
		return model.RelayRoute{}, bindingConflict("relay_identity", id, "Relay ID does not match its map key.")
	}
	return relay, nil
}

func validateBindingServer(data model.PanelData, id string) error {
	server, ok := data.Servers[id]
	if id == "" || !ok || server.ID != id {
		return bindingConflict("server_parent", id, "The parent server is missing or inconsistent; no automatic repair was performed.")
	}
	return nil
}

func validateBindingNode(data model.PanelData, id string) error {
	node, ok := data.Nodes[id]
	if id == "" || !ok {
		return ErrBindingTargetMissing
	}
	if node.ID != id {
		return bindingConflict("node_identity", id, "Node ID does not match its map key.")
	}
	return validateBindingServer(data, node.ServerID)
}

type relayOwnership struct {
	server, route, landing, node string
	host, username, password     string
	port                         int
	udp                          bool
}

// Interpret legacy modes without changing the stored record or generating configuration.
func effectiveRelayOwnership(relay model.RelayRoute) relayOwnership {
	o := relayOwnership{server: strings.TrimSpace(relay.RelayServerID), route: strings.ToLower(strings.TrimSpace(relay.RouteMode)), node: strings.TrimSpace(relay.LandingNodeID)}
	if o.route == "" {
		o.route = "tcp_forward"
	}
	if o.route == "socks5_route" {
		o.landing = strings.ToLower(strings.TrimSpace(relay.LandingMode))
		if o.landing == "" {
			if o.node != "" {
				o.landing = "panel_node"
			} else {
				o.landing = "manual_socks5"
			}
		}
		if o.landing == "manual_socks5" {
			o.host, o.username, o.password = strings.TrimSpace(relay.ManualSocksHost), strings.TrimSpace(relay.ManualSocksUsername), strings.TrimSpace(relay.ManualSocksPassword)
			o.port, o.udp = relay.ManualSocksPort, relay.ManualSocksUDP
		}
	}
	return o
}

func validateRelayParents(data model.PanelData, relay model.RelayRoute) error {
	if err := validateBindingServer(data, relay.RelayServerID); err != nil {
		return err
	}
	o := effectiveRelayOwnership(relay)
	if o.route != "tcp_forward" && o.route != "socks5_route" {
		return bindingConflict("relay_mode", relay.ID, "Relay route mode is invalid.")
	}
	if o.route == "socks5_route" && o.landing != "panel_node" && o.landing != "manual_socks5" {
		return bindingConflict("relay_mode", relay.ID, "Relay landing mode is invalid.")
	}
	// Even an inactive legacy landing reference must not bypass parent identity checks.
	if relay.LandingNodeID != "" || o.route == "tcp_forward" || o.landing == "panel_node" {
		return validateBindingNode(data, relay.LandingNodeID)
	}
	return nil
}

func (s *Store) ValidateRelayParentsLocked(relay model.RelayRoute) error {
	return validateRelayParents(s.Data, relay)
}

func (s *Store) relayReferencesLocked(id string) []BindingConflict {
	conflicts := make([]BindingConflict, 0)
	for clientID, client := range s.Data.Clients {
		for _, relayID := range client.RelayRouteIDs {
			if relayID == id {
				conflicts = append(conflicts, BindingConflict{Kind: "client_relay_reference", ResourceID: clientID, Message: "A client still references this relay, including disabled or expired clients; explicitly resolve the binding first."})
				break
			}
		}
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].ResourceID < conflicts[j].ResourceID })
	return conflicts
}

func validateNewRelay(data model.PanelData, relay model.RelayRoute) error {
	if relay.ID == "" {
		return bindingConflict("relay_identity", "", "A new relay requires an ID.")
	}
	if _, exists := data.RelayRoutes[relay.ID]; exists {
		return bindingConflict("relay_identity", relay.ID, "The relay ID is already in use.")
	}
	return validateRelayParents(data, relay)
}

func (s *Store) CreateRelayLocked(relay model.RelayRoute, actor, ip string) error {
	if err := validateNewRelay(s.Data, relay); err != nil {
		return err
	}
	next := clonePanelData(s.Data)
	if next.RelayRoutes == nil {
		next.RelayRoutes = make(map[string]model.RelayRoute)
	}
	next.RelayRoutes[relay.ID] = relay
	addLog(&next, actor, "relay.create", ip, relay.Name)
	return s.commitLocked(next)
}

func (s *Store) UpdateRelayLocked(id string, relay model.RelayRoute, actor, ip string) error {
	current, err := s.RelayLocked(id)
	if err != nil {
		return err
	}
	if relay.ID != id {
		return bindingConflict("relay_identity", id, "The proposed relay ID does not match the update target.")
	}
	if effectiveRelayOwnership(current) != effectiveRelayOwnership(relay) {
		if conflicts := s.relayReferencesLocked(id); len(conflicts) > 0 {
			return &BindingConflictError{Conflicts: conflicts}
		}
	}
	if err := validateRelayParents(s.Data, current); err != nil {
		return err
	}
	if err := validateRelayParents(s.Data, relay); err != nil {
		return err
	}
	relay.CreatedAt = current.CreatedAt
	next := clonePanelData(s.Data)
	next.RelayRoutes[id] = relay
	addLog(&next, actor, "relay.update", ip, id)
	return s.commitLocked(next)
}

func (s *Store) DeleteRelayLocked(id, actor, ip string) error {
	if _, err := s.RelayLocked(id); err != nil {
		return err
	}
	if conflicts := s.relayReferencesLocked(id); len(conflicts) > 0 {
		return &BindingConflictError{Conflicts: conflicts}
	}
	next := clonePanelData(s.Data)
	delete(next.RelayRoutes, id)
	addLog(&next, actor, "relay.delete", ip, id)
	return s.commitLocked(next)
}
