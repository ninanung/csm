package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// emptyReason classifies why csm has nothing to show.
type emptyReason int

const (
	emptyNoClaude      emptyReason = iota // `claude` binary missing
	emptyNoProjectsDir                    // ~/.claude/projects doesn't exist
	emptyNoSessions                       // dir exists but no .jsonl files
)

// preflight runs cheap environment checks before we attempt to load sessions.
// Returns the matching emptyReason and `false` when csm can't proceed; the
// caller should render the empty state and exit. When everything looks fine it
// returns 0, true.
func preflight() (emptyReason, bool) {
	if _, err := exec.LookPath("claude"); err != nil {
		return emptyNoClaude, false
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return emptyNoClaude, false
	}
	projectsDir := filepath.Join(home, ".claude", "projects")
	info, err := os.Stat(projectsDir)
	if err != nil || !info.IsDir() {
		return emptyNoProjectsDir, false
	}

	return 0, true
}

// printEmptyState renders the friendly "no work to show" screen — logo + a
// short, localised explanation pointing the user at the right next step.
func printEmptyState(w io.Writer, reason emptyReason) {
	var titleKey, hintKey string
	switch reason {
	case emptyNoClaude:
		titleKey, hintKey = "empty.no_claude.title", "empty.no_claude.hint"
	case emptyNoProjectsDir:
		titleKey, hintKey = "empty.no_projects_dir.title", "empty.no_projects_dir.hint"
	case emptyNoSessions:
		titleKey, hintKey = "empty.no_sessions.title", "empty.no_sessions.hint"
	default:
		return
	}

	fmt.Fprintln(w)
	for _, line := range strings.Split(logoArt, "\n") {
		fmt.Fprintln(w, "  "+styleLogo.Render(line))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  "+styleTagline.Render(T(titleKey)))
	fmt.Fprintln(w, "  "+styleVersion.Render(T(hintKey)))
	fmt.Fprintln(w)
}
