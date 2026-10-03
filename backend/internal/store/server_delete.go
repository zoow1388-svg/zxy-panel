// SPDX-License-Identifier: AGPL-3.0-only
package store

import "errors"

// Caller must hold Mu.Lock and verify deletion guards before calling.
func (s *Store) DeleteServerLocked(id, actor, ip string) error {
	server, exists := s.Data.Servers[id]
	if !exists || server.ID != id {
		return errors.New("server identity missing or inconsistent")
	}
	next := clonePanelData(s.Data)
	delete(next.Servers, id)
	addLog(&next, actor, "server.delete", ip, id)
	return s.commitLocked(next)
}
