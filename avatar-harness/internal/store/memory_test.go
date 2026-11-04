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

func TestCorrectionMemoryPersistsAndOwnsReferenceSlices(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	sid := s.Create("role").ID
	s.AddHistory(sid, []map[string]any{{"role": "user", "content": "愿意分析"}, {"role": "user", "content": "这次不要分析"}})
	ref := MemoryReference{Message: 1, Quote: "愿意分析"}
	mem := SessionMemory{Through: 2, Entries: []MemoryEntry{{Kind: "correction", Message: 2, Quote: "这次不要分析", Replaces: []MemoryReference{ref}}}, Superseded: []MemoryReference{ref}}
	if !s.SetMemory(sid, 2, mem) {
		t.Fatal("memory not committed")
	}
	mem.Entries[0].Replaces[0].Quote = "caller mutation"
	mem.Superseded[0].Quote = "caller mutation"
	snap := s.Conversation(sid, 2)
	snap.Memory.Entries[0].Replaces[0].Quote = "snapshot mutation"
	snap.Memory.Superseded[0].Quote = "snapshot mutation"
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	actual := s.Conversation(sid, 2).Memory
	if actual.Entries[0].Replaces[0] != ref || actual.Superseded[0] != ref {
		t.Fatal("reference ownership or persistence failed", actual)
	}
}
