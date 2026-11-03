package store

import (
	"encoding/json"
	"os"
	"path/filepath"

	"avatarharness/internal/config"
)

func (s *Store) CharacterAssetDir() string {
	return filepath.Join(filepath.Dir(s.file.Name()), "characters")
}

func (s *Store) Characters() []config.Character {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]config.Character(nil), s.characters...)
}

// Publish only after the durable list has been replaced successfully.
func (s *Store) AddCharacter(ch config.Character) (config.Character, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.characters {
		if existing.ID == ch.ID {
			return existing, false, nil
		}
	}
	items := append(append([]config.Character(nil), s.characters...), ch)
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return config.Character{}, false, err
	}
	path := filepath.Join(filepath.Dir(s.file.Name()), "characters.json")
	if err := os.WriteFile(path+".tmp", append(raw, '\n'), 0600); err != nil {
		return config.Character{}, false, err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return config.Character{}, false, err
	}
	s.characters = items
	return ch, true, nil
}
