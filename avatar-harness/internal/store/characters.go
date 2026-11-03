package store

import (
	"encoding/json"
	"errors"
	"fmt"
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
	items := append([]config.Character(nil), s.characters...)
	for i := range items {
		items[i].Custom = true
	}
	return items
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
	if err := s.saveCharacters(items); err != nil {
		return config.Character{}, false, err
	}
	s.characters = items
	return ch, true, nil
}

// Caller holds s.mu; no in-memory changes until the replacement succeeds.
func (s *Store) saveCharacters(items []config.Character) error {
	raw, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(s.file.Name()), "characters.json")
	if err := os.WriteFile(path+".tmp", append(raw, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return err
	}
	return nil
}

// Only the library-owned ZIP is removed. Source photos and creator exports
// are outside this store's ownership. Existing conversations keep their IDs.
func (s *Store) DeleteCharacter(id, fallback string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	for i, ch := range s.characters {
		if ch.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return false, nil
	}
	ch := s.characters[index]
	asset := filepath.Join(s.CharacterAssetDir(), id+".zip")
	if filepath.Base(id) != id || filepath.Clean(ch.AssetPath) != asset {
		return false, fmt.Errorf("角色资产不在自定义角色目录中")
	}
	// Stage the asset so a failed catalog update can restore the character.
	staged := asset + ".deleting"
	moved := false
	if err := os.Rename(asset, staged); err == nil {
		moved = true
	} else if !os.IsNotExist(err) {
		return false, err
	}
	items := append([]config.Character{}, s.characters[:index]...)
	items = append(items, s.characters[index+1:]...)
	if err := s.saveCharacters(items); err != nil {
		if moved {
			err = errors.Join(err, os.Rename(staged, asset))
		}
		return false, err
	}
	s.characters = items
	for sid, session := range s.sessions {
		if session.CharacterID == id {
			session.CharacterID = fallback
			s.sessions[sid] = session
			s.append(record{Kind: "session", Session: &session})
		}
	}
	s.flush()
	if moved {
		return true, os.Remove(staged)
	}
	return true, nil
}
