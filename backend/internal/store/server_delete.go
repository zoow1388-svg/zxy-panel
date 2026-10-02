// SPDX-License-Identifier: AGPL-3.0-only
package store

import "maps"

// Caller must hold Mu.Lock and verify deletion guards before calling.
func (s *Store) DeleteServerLocked(id, actor, ip string) error {
	previous := s.Data
	// Isolate maps changed by deletion, logging and SaveLocked normalization.
	s.Data.Servers = maps.Clone(previous.Servers)
	s.Data.OperationLogs = maps.Clone(previous.OperationLogs)
	s.Data.Admins = maps.Clone(previous.Admins)
	delete(s.Data.Servers, id)
	s.AddLog(actor, "server.delete", ip, id)
	if err := s.SaveLocked(); err != nil {
		s.Data = previous
		return err
	}
	return nil
}
