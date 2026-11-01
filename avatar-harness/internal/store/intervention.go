package store

import "avatarharness/internal/intervention"

func (s *Store) Intervention(sid string) intervention.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.interventions[sid].Clone()
}

// CommitIntervention atomically journals state and any associated final history.
// A stale model result cannot advance a newer UI or conversation revision.
func (s *Store) CommitIntervention(sid string, expected uint64, next intervention.State, messages []map[string]any) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sid]; !ok || s.interventions[sid].Version != expected || next.Version != expected+1 {
		return false
	}
	s.interventions[sid] = next.Clone()
	s.history[sid] = append(s.history[sid], messages...)
	s.append(record{Kind: "intervention", SessionID: sid, Intervention: &next, Messages: messages})
	return true
}
