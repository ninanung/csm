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
// to ~/Documents/csm-exports/.
func ExportSessionToFile(s Session, outDir string) (string, error) {
	if outDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		outDir = filepath.Join(home, "Documents", "csm-exports")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(outDir, exportFileName(s))
	if err := copyExportFile(s.Path, path); err != nil {
		return "", err
	}
	return path, nil
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
