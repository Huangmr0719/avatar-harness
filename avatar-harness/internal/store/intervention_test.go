package store

import (
	"avatarharness/internal/intervention"
	"testing"
	"time"
)

func TestInterventionHistoryAtomicCASRestoreAndSessionIsolation(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	sid := s.Create("role").ID
	other := s.Create("role").ID
	next, err := intervention.Apply(intervention.State{}, intervention.Command{Operation: "start", Mode: intervention.CBT}, time.Now(), "e")
	if err != nil {
		t.Fatal(err)
	}
	if !s.CommitIntervention(sid, 0, next, []map[string]any{{"role": "user", "content": "记录"}}) {
		t.Fatal("initial commit")
	}
	if s.CommitIntervention(sid, 0, next, []map[string]any{{"role": "user", "content": "stale"}}) || len(s.History(sid, 10)) != 1 {
		t.Fatal("stale history committed")
	}
	view := s.Intervention(sid)
	view.Facts["need"] = intervention.Evidence{Quote: "mutation"}
	if len(s.Intervention(sid).Facts) != 0 || s.Intervention(other).Active() {
		t.Fatal("snapshot or session leak")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Intervention(sid).Mode != intervention.CBT || len(s.History(sid, 10)) != 1 {
		t.Fatal("restore lost atomic pair")
	}
	s.Clear(sid)
	s.Close()
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Intervention(sid).Active() || len(s.History(sid, 10)) != 0 {
		t.Fatal("clear did not persist")
	}
}
