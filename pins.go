package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// PinEntry is one row of pins.json. Forward-compatible — extra fields are
// silently preserved when we re-write the file.
type PinEntry struct {
	ID       string    `json:"id"`
	Label    string    `json:"label,omitempty"`
	PinnedAt time.Time `json:"pinned_at"`
}

type pinStore struct {
	Pinned []PinEntry `json:"pinned"`
}

func pinsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "csm", "pins.json"), nil
}

// LoadPins reads the sidecar pin store. Missing file → empty store, no error.
func LoadPins() (pinStore, error) {
	var store pinStore
	p, err := pinsPath()
	if err != nil {
		return store, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return store, err
	}
	if len(b) == 0 {
		return store, nil
	}
	if err := json.Unmarshal(b, &store); err != nil {
		return store, err
	}
	return store, nil
}

// SavePins writes the store atomically (temp file + rename).
func SavePins(store pinStore) error {
	p, err := pinsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	b, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// IsPinned returns true when id is present in the store.
func (s pinStore) IsPinned(id string) bool {
	for _, e := range s.Pinned {
		if e.ID == id {
			return true
		}
	}
	return false
}

// Toggle adds the id if missing, or removes it if present. Returns the new
// "pinned?" state.
func (s *pinStore) Toggle(id, label string) bool {
	for i, e := range s.Pinned {
		if e.ID == id {
			s.Pinned = append(s.Pinned[:i], s.Pinned[i+1:]...)
			return false
		}
	}
	s.Pinned = append(s.Pinned, PinEntry{
		ID:       id,
		Label:    label,
		PinnedAt: time.Now(),
	})
	return true
}

// idSet returns a set of pinned IDs for quick lookup during render.
func (s pinStore) idSet() map[string]struct{} {
	m := make(map[string]struct{}, len(s.Pinned))
	for _, e := range s.Pinned {
		m[e.ID] = struct{}{}
	}
	return m
}
