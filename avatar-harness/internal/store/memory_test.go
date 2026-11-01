package store

import "testing"

func TestSessionMemoryPersistsWithSourcesAndClear(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	sid := s.Create("role").ID
	other := s.Create("role").ID
	s.AddHistory(sid, []map[string]any{{"role": "user", "content": "每天只有十五分钟"}, {"role": "assistant", "content": "记住了。"}})
	memory := SessionMemory{Through: 2, Entries: []MemoryEntry{{Kind: "constraint", Message: 1, Quote: "每天只有十五分钟"}}}
	if !s.SetMemory(sid, 2, memory) || s.SetMemory(sid, 3, memory) {
		t.Fatal("memory source revision was not checked")
	}
	memory.Entries[0].Quote = "changed by caller"
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	c := s.Conversation(sid, 1)
	if c.Memory.Through != 2 || c.Memory.Entries[0].Quote != "每天只有十五分钟" || len(c.Archived) != 2 {
		t.Fatal(c)
	}
	c.Memory.Entries[0].Quote = "changed snapshot"
	if s.Conversation(sid, 20).Memory.Entries[0].Quote != "每天只有十五分钟" || len(s.Conversation(other, 20).Memory.Entries) != 0 {
		t.Fatal("memory snapshot leaked to another reader or session")
	}
	s.Clear(sid)
	if s.SetMemory(sid, 2, memory) {
		t.Fatal("cleared history accepted stale memory")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if c := s.Conversation(sid, 20); len(c.Memory.Entries) != 0 || len(c.Recent) != 0 || len(c.Archived) != 0 {
		t.Fatal("clear did not persist", c)
	}
}
