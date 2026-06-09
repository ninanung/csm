package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Export — for csm, "export" means copying the source JSONL session file
// verbatim to a destination. No conversion, no filtering. The user gets the
// exact same bytes Claude Code wrote, which keeps the output round-trippable
// for backups and re-imports.

// ExportSessionToFile copies s.Path into outDir under a stable, slugged
// filename and returns the absolute output path. If outDir is empty, defaults
// to the OS Downloads folder (~/Downloads on macOS/Linux).
//
// When the session has a sibling "<uuid>/subagents/" directory, the export
// becomes a FOLDER (named with the slug) that mirrors the on-disk layout:
//
//	<slug>/
//	  ├── <uuid>.jsonl
//	  └── <uuid>/subagents/agent-*.jsonl
//
// so the export is round-trippable into ~/.claude/projects/<encoded-cwd>/.
func ExportSessionToFile(s Session, outDir string) (string, error) {
	if outDir == "" {
		dir, err := defaultDownloadsDir()
		if err != nil {
			return "", err
		}
		outDir = dir
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}

	if hasSubagentDir(s.Path) {
		bundleDir := filepath.Join(outDir, exportBundleName(s))
		if err := os.MkdirAll(bundleDir, 0o755); err != nil {
			return "", err
		}
		if err := copyExportFile(s.Path, filepath.Join(bundleDir, filepath.Base(s.Path))); err != nil {
			return "", err
		}
		uuid := strings.TrimSuffix(filepath.Base(s.Path), ".jsonl")
		src := filepath.Join(filepath.Dir(s.Path), uuid)
		dst := filepath.Join(bundleDir, uuid)
		if err := copyDir(src, dst); err != nil {
			return "", err
		}
		return bundleDir, nil
	}

	path := filepath.Join(outDir, exportFileName(s))
	if err := copyExportFile(s.Path, path); err != nil {
		return "", err
	}
	return path, nil
}

// hasSubagentDir reports whether the given session jsonl has a sibling
// "<uuid>/" directory worth bundling into the export. Claude Code stores
// per-session sidecars (subagents/, tool-results/, …) here. Returns true
// only when the directory has at least one entry — empty stubs aren't
// worth turning the export into a folder.
func hasSubagentDir(jsonlPath string) bool {
	uuid := strings.TrimSuffix(filepath.Base(jsonlPath), ".jsonl")
	dir := filepath.Join(filepath.Dir(jsonlPath), uuid)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// exportBundleName returns the folder name for a session export bundle.
// Same convention as exportFileName but without the .jsonl extension —
// the directory contains the jsonl plus a sibling subagents tree.
func exportBundleName(s Session) string {
	return strings.TrimSuffix(exportFileName(s), ".jsonl")
}

// defaultDownloadsDir returns the conventional OS download directory
// (~/Downloads on macOS and most Linux desktops). Falls back to $HOME if
// Downloads doesn't exist for any reason.
func defaultDownloadsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(home, "Downloads")
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return candidate, nil
	}
	return home, nil
}

// CopySession copies a session's JSONL bytes verbatim into w. Used by
// csm export -o - (stdout) and by the bulk download wrapper.
func CopySession(w io.Writer, s Session) error {
	src, err := os.Open(s.Path)
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = io.Copy(w, src)
	return err
}

func copyExportFile(src, dst string) error {
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

// ---- file-name helpers (shared with download) ----

func exportFileName(s Session) string {
	date := "session"
	if !s.LastActivity.IsZero() {
		date = s.LastActivity.Format("2006-01-02")
	}
	parts := []string{date}
	if s.Project != "" {
		parts = append(parts, slug(s.Project))
	}
	if s.FirstMessage != "" {
		parts = append(parts, slug(oneLine(s.FirstMessage)))
	}
	name := strings.Join(parts, "-")
	const maxRunes = 80
	runes := []rune(name)
	if len(runes) > maxRunes {
		name = string(runes[:maxRunes])
	}
	return name + ".jsonl"
}

var slugSafeRE = regexp.MustCompile(`[^a-z0-9가-힣-]+`)

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugSafeRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	const maxRunes = 40
	runes := []rune(s)
	if len(runes) > maxRunes {
		s = string(runes[:maxRunes])
		s = strings.TrimRight(s, "-")
	}
	return s
}

func oneLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			return l
		}
	}
	return ""
}
