package store

// MemoryReference identifies a specific original phrase, not an entire user
// message: correcting a budget must not erase the time limit in that message.
type MemoryReference struct {
	Message int    `json:"message"`
	Quote   string `json:"quote"`
}

// MemoryEntry retains a user's own words. Context is the preceding assistant
// reply when it is needed to understand an agreement, never generated analysis.
type MemoryEntry struct {
	Kind    string `json:"kind"`
	Message int    `json:"message"`
	Quote   string `json:"quote"`
	Context string `json:"context,omitempty"`
	// Only correction entries may replace older user phrases. History stays
	// intact; retrieval excludes those phrases and retains unaffected facts.
	Replaces []MemoryReference `json:"replaces,omitempty"`
}

type SessionMemory struct {
	Through int           `json:"through"`
	Entries []MemoryEntry `json:"entries"`
	// Retain invalidations even if a later bounded selector omits a correction.
	Superseded []MemoryReference `json:"superseded,omitempty"`
}

type Conversation struct {
	Recent   []map[string]any
	Archived []map[string]any
	Memory   SessionMemory
}

// Conversation snapshots memory and history under the same lock. Historical
// messages are immutable after AddHistory, matching History's existing contract.
func (s *Store) Conversation(sid string, limit int) Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.history[sid]
	start := historyStart(all, limit)
	memory := s.memories[sid]
	memory = cloneMemory(memory)
	return Conversation{
		Recent:   append([]map[string]any{}, all[start:]...),
		Archived: append([]map[string]any{}, all[:start]...),
		Memory:   memory,
	}
}

// SetMemory rejects a summary if the source history changed while it was being
// selected. Runtime additionally checks the originating turn and cancellation.
func (s *Store) SetMemory(sid string, historyCount int, memory SessionMemory) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[sid]; !ok || len(s.history[sid]) != historyCount || memory.Through > historyCount || memory.Through < s.memories[sid].Through {
		return false
	}
	memory = cloneMemory(memory)
	s.memories[sid] = memory
	s.append(record{Kind: "memory", SessionID: sid, Memory: &memory})
	return true
}

func cloneMemory(memory SessionMemory) SessionMemory {
	memory.Entries = append([]MemoryEntry{}, memory.Entries...)
	for i := range memory.Entries {
		memory.Entries[i].Replaces = append([]MemoryReference(nil), memory.Entries[i].Replaces...)
	}
	memory.Superseded = append([]MemoryReference(nil), memory.Superseded...)
	return memory
}
