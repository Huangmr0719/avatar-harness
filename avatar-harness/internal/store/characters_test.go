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

func TestFailedDeleteRestoresOwnedAssetAndRejectsOutsideAssets(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := os.MkdirAll(s.CharacterAssetDir(), 0700); err != nil {
		t.Fatal(err)
	}
	asset := filepath.Join(s.CharacterAssetDir(), "custom.zip")
	if err := os.WriteFile(asset, []byte("owned avatar"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddCharacter(config.Character{ID: "custom", AssetPath: asset}); err != nil {
		t.Fatal(err)
	}
	catalog := filepath.Join(dir, "characters.json")
	if err := os.Rename(catalog, catalog+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(catalog, 0700); err != nil {
		t.Fatal(err)
	}
	if deleted, err := s.DeleteCharacter("custom", "default"); deleted || err == nil {
		t.Fatal("failed catalog update reported deletion", deleted, err)
	}
	if raw, err := os.ReadFile(asset); err != nil || string(raw) != "owned avatar" || len(s.Characters()) != 1 {
		t.Fatal("failed deletion lost the original avatar", err)
	}
	if err := os.Remove(catalog); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(dir, "protected.zip")
	if err := os.WriteFile(outside, []byte("source photo/export placeholder"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.AddCharacter(config.Character{ID: "outside", AssetPath: outside}); err != nil {
		t.Fatal(err)
	}
	if deleted, err := s.DeleteCharacter("outside", "default"); deleted || err == nil {
		t.Fatal("outside asset accepted", deleted, err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("protected source removed", err)
	}
}
