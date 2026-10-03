// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"slices"
	"strings"

	"zxy-panel/backend/internal/model"
)

// Caller holds Mu.Lock through validation, candidate generation and commit.
func (s *Store) ClientLocked(id string) (model.Client, error) {
	client, ok := s.Data.Clients[id]
	if id == "" || !ok {
		return model.Client{}, ErrClientNotFound
	}
	if client.ID != id {
		return model.Client{}, bindingConflict("client_identity", id, "Client ID does not match its map key.")
	}
	return client, nil
}

func validateClientBindings(data model.PanelData, client model.Client) error {
	for _, id := range client.NodeIDs {
		if strings.TrimSpace(id) == "" {
			return ErrBindingTargetMissing
		}
		if err := validateBindingNode(data, id); err != nil {
			return err
		}
	}
	for _, id := range client.RelayRouteIDs {
		relay, ok := data.RelayRoutes[id]
		if strings.TrimSpace(id) == "" || !ok {
			return ErrBindingTargetMissing
		}
		if relay.ID != id {
			return bindingConflict("relay_identity", id, "Relay ID does not match its map key.")
		}
		if err := validateRelayParents(data, relay); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ValidateClientBindingsLocked(client model.Client) error {
	return validateClientBindings(s.Data, client)
}

func ownClientBindings(client model.Client) model.Client {
	client.NodeIDs = slices.Clone(client.NodeIDs)
	client.RelayRouteIDs = slices.Clone(client.RelayRouteIDs)
	return client
}

func validateNewClient(data model.PanelData, client model.Client) error {
	if client.ID == "" {
		return bindingConflict("client_identity", "", "A new client requires an ID.")
	}
	if _, exists := data.Clients[client.ID]; exists {
		return bindingConflict("client_identity", client.ID, "The client ID is already in use.")
	}
	return validateClientBindings(data, client)
}

func (s *Store) CreateClientLocked(client model.Client, actor, ip string) error {
	if err := validateNewClient(s.Data, client); err != nil {
		return err
	}
	next := clonePanelData(s.Data)
	if next.Clients == nil {
		next.Clients = make(map[string]model.Client)
	}
	next.Clients[client.ID] = ownClientBindings(client)
	addLog(&next, actor, "client.create", ip, client.Username)
	return s.commitLocked(next)
}

func (s *Store) UpdateClientLocked(id string, client model.Client, actor, ip string) error {
	current, err := s.ClientLocked(id)
	if err != nil {
		return err
	}
	if client.ID != id {
		return bindingConflict("client_identity", id, "The proposed client ID does not match the update target.")
	}
	if err := validateClientBindings(s.Data, client); err != nil {
		return err
	}
	client.CreatedAt = current.CreatedAt
	next := clonePanelData(s.Data)
	next.Clients[id] = ownClientBindings(client)
	addLog(&next, actor, "client.update", ip, id)
	return s.commitLocked(next)
}

func (s *Store) DeleteClientLocked(id, actor, ip string) error {
	if _, err := s.ClientLocked(id); err != nil {
		return err
	}
	next := clonePanelData(s.Data)
	delete(next.Clients, id)
	addLog(&next, actor, "client.delete", ip, id)
	return s.commitLocked(next)
}

func (s *Store) CreateClientRelayLocked(client model.Client, relay model.RelayRoute, actor, ip, detail string) error {
	if err := validateNewRelay(s.Data, relay); err != nil {
		return err
	}
	next := clonePanelData(s.Data)
	if next.RelayRoutes == nil {
		next.RelayRoutes = make(map[string]model.RelayRoute)
	}
	next.RelayRoutes[relay.ID] = relay
	// Validate against the staged relay, never temporarily publish it in live data.
	if err := validateNewClient(next, client); err != nil {
		return err
	}
	if next.Clients == nil {
		next.Clients = make(map[string]model.Client)
	}
	next.Clients[client.ID] = ownClientBindings(client)
	addLog(&next, actor, "client.create_relay", ip, detail)
	return s.commitLocked(next)
}
