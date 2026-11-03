package store

import (
	"os"
	"path/filepath"
	"testing"

	"avatarharness/internal/config"
)

func TestFailedCharacterSaveDoesNotPublish(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.Mkdir(filepath.Join(dir, "characters.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddCharacter(config.Character{ID: "new"}); err == nil {
		t.Fatal("failed persistence was reported as success")
	}
	if len(s.Characters()) != 0 {
		t.Fatal("failed save became selectable")
	}
}
