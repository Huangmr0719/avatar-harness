package store

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLegacyReadOnlyImportMergesAndDoesNotRepeat(t *testing.T) {
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("optional migration CLI unavailable")
	}
	dir := t.TempDir()
	s, _ := Open(dir)
	current := s.Create("role-new")
	_ = s.Close()
	path := filepath.Join(dir, "sessions.sqlite3")
	sql := `CREATE TABLE sessions(id TEXT,character_id TEXT,title TEXT,created_at REAL);CREATE TABLE events(seq INTEGER,session_id TEXT,turn_id TEXT,type TEXT,at REAL,data TEXT);CREATE TABLE history(id INTEGER,session_id TEXT,message TEXT);INSERT INTO sessions VALUES('legacy','role-old','旧会话',1);INSERT INTO events VALUES(13,'legacy','old-turn','user.message',1,'{"text":"old"}');INSERT INTO history VALUES(1,'legacy','{"role":"user","content":"old"}');`
	if output, err := exec.Command(bin, path, sql).CombinedOutput(); err != nil {
		t.Fatal(string(output), err)
	}
	before, _ := os.ReadFile(path)
	digest := sha256.Sum256(before)
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Sessions()) != 2 || len(s.Events("legacy", 0)) != 1 || len(s.History("legacy", 20)) != 1 {
		t.Fatal("legacy data not merged")
	}
	if _, ok := s.Session(current.ID); !ok {
		t.Fatal("new Go session lost")
	}
	_ = s.Close()
	after, _ := os.ReadFile(path)
	if sha256.Sum256(after) != digest {
		t.Fatal("source SQLite changed")
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(s.Events("legacy", 0)) != 1 {
		t.Fatal("migration duplicated")
	}
}

func TestJournalPersistsHistoryWithoutMediaAndClear(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	session := s.Create("role")
	_, queue, unsubscribe := s.Subscribe(session.ID)
	s.Emit(session.ID, "turn", "pipeline.audio", map[string]any{"audio_base64": "AAAA", "pts_ms": 0})
	event := <-queue
	if event.Data["audio_base64"] != "AAAA" {
		t.Fatal("live audio lost")
	}
	unsubscribe()
	s.AddHistory(session.ID, []map[string]any{{"role": "user", "content": "hello"}})
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	events := s.Events(session.ID, 0)
	if events[0].Data["audio_base64"] != nil {
		t.Fatal("audio persisted")
	}
	if len(s.History(session.ID, 20)) != 1 {
		t.Fatal("history missing")
	}
	s.Clear(session.ID)
	if len(s.Events(session.ID, 0)) != 0 || len(s.History(session.ID, 20)) != 0 {
		t.Fatal("clear failed")
	}
}
func TestSlowSubscriberNeverBlocksKernel(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()
	session := s.Create("role")
	_, queue, unsubscribe := s.Subscribe(session.ID)
	defer unsubscribe()
	for i := 0; i < 400; i++ {
		s.Emit(session.ID, "turn", "diagnostic", map[string]any{"index": i})
	}
	count := 0
	for range queue {
		count++
	}
	if count != 256 {
		t.Fatal(count)
	}
}
