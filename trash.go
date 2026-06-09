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
// the original project subpath so a restore can put it back. The sibling
// "<uuid>/" sub-agent directory (if present) is moved alongside it so the
// session's data stays together. Returns the new jsonl path on success.
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
	moveSubagentDir(s.Path, destDir)
	return dest, nil
}

// moveSubagentDir moves the "<uuid>/" directory sibling of a session JSONL
// into destParent (e.g. for trash or restore). Silently no-ops when the
// sibling directory does not exist. Cross-FS rename falls back to copy.
func moveSubagentDir(jsonlPath, destParent string) {
	uuid := strings.TrimSuffix(filepath.Base(jsonlPath), ".jsonl")
	srcDir := filepath.Join(filepath.Dir(jsonlPath), uuid)
	info, err := os.Stat(srcDir)
	if err != nil || !info.IsDir() {
		return
	}
	destDir := filepath.Join(destParent, uuid)
	if err := os.Rename(srcDir, destDir); err == nil {
		return
	}
	// Cross-filesystem fallback: copy tree then remove.
	if err := copyDir(srcDir, destDir); err == nil {
		_ = os.RemoveAll(srcDir)
	}
}

// copyDir recursively copies a directory tree. Used as a rename fallback.
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, srcInfo.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		sp := filepath.Join(src, e.Name())
		dp := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(sp, dp); err != nil {
				return err
			}
		} else {
			if err := copyFile(sp, dp); err != nil {
				return err
			}
		}
	}
	return nil
}

// RestoreFromTrash moves a session back from the trash to its original project
// directory. Path is the trash-side absolute path. The sibling "<uuid>/"
// sub-agent directory (if present in trash) is restored alongside it.
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
	moveSubagentDir(trashPath, destDir)
	return nil
}

// PermanentlyDelete removes a session JSONL from disk for good. The sibling
// "<uuid>/" sub-agent directory (if present) is removed alongside it.
func PermanentlyDelete(path string) error {
	err := os.Remove(path)
	uuid := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	siblingDir := filepath.Join(filepath.Dir(path), uuid)
	if info, e := os.Stat(siblingDir); e == nil && info.IsDir() {
		_ = os.RemoveAll(siblingDir)
	}
	return err
}

// CleanupOrphanSubagentDirs scans the live projects tree for "<uuid>/" sub-agent
// directories whose corresponding "<uuid>.jsonl" lives in the trash (i.e. the
// directory was left behind by an older MoveToTrash that only handled the
// jsonl). Moves them into the trash alongside their jsonl. Returns the count
// of directories consolidated.
//
// Safe to run on every startup: when there are no orphans, the scan is a few
// readdirs and returns 0.
func CleanupOrphanSubagentDirs() (int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}
	td, err := trashDir()
	if err != nil {
		return 0, err
	}
	projRoot := filepath.Join(home, ".claude", "projects")

	// Build set of trashed jsonl IDs keyed by project name so we only match
	// dirs whose corresponding session has actually been trashed.
	trashed := map[string]map[string]bool{}
	trashProjects, err := os.ReadDir(td)
	if err != nil {
		return 0, err
	}
	for _, tp := range trashProjects {
		if !tp.IsDir() {
			continue
		}
		ids := map[string]bool{}
		files, err := os.ReadDir(filepath.Join(td, tp.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".jsonl") {
				ids[strings.TrimSuffix(f.Name(), ".jsonl")] = true
			}
		}
		trashed[tp.Name()] = ids
	}

	moved := 0
	projects, err := os.ReadDir(projRoot)
	if err != nil {
		return 0, err
	}
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		ids, ok := trashed[p.Name()]
		if !ok || len(ids) == 0 {
			continue
		}
		projDir := filepath.Join(projRoot, p.Name())
		trashProjDir := filepath.Join(td, p.Name())
		entries, err := os.ReadDir(projDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || !ids[e.Name()] {
				continue
			}
			src := filepath.Join(projDir, e.Name())
			dst := filepath.Join(trashProjDir, e.Name())
			if _, statErr := os.Stat(dst); statErr == nil {
				// Already exists in trash — remove the live orphan.
				_ = os.RemoveAll(src)
				moved++
				continue
			}
			if err := os.Rename(src, dst); err == nil {
				moved++
			} else if err := copyDir(src, dst); err == nil {
				_ = os.RemoveAll(src)
				moved++
			}
		}
	}
	return moved, nil
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
