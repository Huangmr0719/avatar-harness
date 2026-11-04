package store

import (
	"testing"
	"time"

	"avatarharness/internal/intervention"
)

func TestHumanisticChoicePersistsAndKeepsMemoryAndSessionIsolation(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	sid, other := s.Create("role").ID, s.Create("role").ID
	s.AddHistory(sid, []map[string]any{{"role": "user", "content": "先听我说"}})
	s.SetMemory(sid, 1, SessionMemory{Through: 1, Entries: []MemoryEntry{{Kind: "support_preference", Message: 1, Quote: "先听我说"}}})
	for _, enabled := range []bool{true, false} {
		before := s.Intervention(sid)
		next, err := intervention.Apply(before, intervention.Command{Operation: "humanistic", Version: before.Version, Enabled: &enabled, Source: "user_control"}, time.Now(), "execution")
		if err != nil || !s.CommitIntervention(sid, before.Version, next, nil) {
			t.Fatal(err)
		}
		view := s.Intervention(sid)
		*view.HumanisticOverride = !enabled
		if *s.Intervention(sid).HumanisticOverride != enabled || s.Intervention(other).HumanisticOverride != nil {
			t.Fatal("choice pointer or session leaked")
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		s, err = Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		state := s.Intervention(sid)
		memory := s.Conversation(sid, 20).Memory
		if state.HumanisticOverride == nil || *state.HumanisticOverride != enabled || state.Active() != enabled || len(s.History(sid, 20)) != 1 || len(memory.Entries) != 1 || memory.Entries[0].Quote != "先听我说" {
			t.Fatal("restart lost choice, history or memory", state, memory)
		}
	}
	s.Close()
}
