package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// trashDir returns the absolute path of the csm trash directory, creating it
// lazily on first call.
func trashDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".claude", "csm", "trash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// MoveToTrash moves a session's JSONL into the csm trash directory, preserving
// the original project subpath so a restore can put it back. Returns the new
// path on success.
func MoveToTrash(s Session) (string, error) {
	if s.Path == "" {
		return "", fmt.Errorf("session has no path")
	}
	td, err := trashDir()
	if err != nil {
		return "", err
	}
	// Preserve the project folder structure: trash/<encoded-cwd>/<id>.jsonl
	parent := filepath.Base(filepath.Dir(s.Path))
	destDir := filepath.Join(td, parent)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, filepath.Base(s.Path))
	if err := os.Rename(s.Path, dest); err != nil {
		// Fall back to copy + delete when rename crosses filesystem boundaries.
		if err := copyFile(s.Path, dest); err != nil {
			return "", err
		}
		_ = os.Remove(s.Path)
	}
	return dest, nil
}

// RestoreFromTrash moves a session back from the trash to its original project
// directory. Path is the trash-side absolute path.
func RestoreFromTrash(trashPath string) error {
	parent := filepath.Base(filepath.Dir(trashPath))
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	destDir := filepath.Join(home, ".claude", "projects", parent)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(destDir, filepath.Base(trashPath))
	if err := os.Rename(trashPath, dest); err != nil {
		if err := copyFile(trashPath, dest); err != nil {
			return err
		}
		_ = os.Remove(trashPath)
	}
	return nil
}

// PermanentlyDelete removes a session JSONL from disk for good.
func PermanentlyDelete(path string) error {
	return os.Remove(path)
}

// LoadTrashSessions scans ~/.claude/csm/trash/ and returns the trashed sessions
// using the same metadata extraction as LoadSessions.
func LoadTrashSessions() ([]Session, error) {
	td, err := trashDir()
	if err != nil {
		return nil, err
	}
	var sessions []Session
	entries, err := os.ReadDir(td)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		projectDir := filepath.Join(td, e.Name())
		files, err := os.ReadDir(projectDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			s, err := loadSession(filepath.Join(projectDir, f.Name()))
			if err != nil {
				continue
			}
			sessions = append(sessions, s)
		}
	}
	return sessions, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
