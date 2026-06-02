package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

const usage = `csm — Claude Code session manager

Usage:
  csm           Interactive picker; on select, exec 'claude --resume <id>' in
                the selected session's cwd. Replaces this process.
  csm --print   Interactive picker; print "<session-id>\t<cwd>" to stdout and
                exit. For multiplexer adapters.
  csm -h        Show this help.

Keys:
  ↑/↓ or j/k    navigate
  enter         select
  /             filter (esc to cancel)
  g/G           jump to first/last session
  q             quit
`

func main() {
	printMode := flag.Bool("print", false, "print selection to stdout instead of exec'ing claude")
	flag.Usage = func() { fmt.Print(usage) }
	flag.Parse()

	sessions, err := LoadSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "csm: failed to load sessions: %v\n", err)
		os.Exit(1)
	}
	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, "csm: no sessions found under ~/.claude/projects")
		os.Exit(1)
	}

	cwd, _ := os.Getwd()
	m := NewModel(sessions, cwd)
	prog := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	final, err := prog.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "csm: %v\n", err)
		os.Exit(1)
	}
	fm := final.(Model)
	if fm.Quit || fm.Selected == nil {
		os.Exit(130)
	}

	sel := fm.Selected

	if *printMode {
		fmt.Printf("%s\t%s\n", sel.ID, sel.CWD)
		return
	}

	// Default mode: cd + exec claude --resume.
	// Validate cwd; if missing, fall back to current cwd with a warning.
	target := sel.CWD
	if target == "" || !dirExists(target) {
		fmt.Fprintf(os.Stderr, "csm: session cwd missing or absent (%q). starting in current dir.\n", target)
		target = cwd
	}
	if err := os.Chdir(target); err != nil {
		fmt.Fprintf(os.Stderr, "csm: chdir failed: %v\n", err)
		os.Exit(1)
	}

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintf(os.Stderr, "csm: 'claude' not found in PATH: %v\n", err)
		os.Exit(1)
	}
	args := []string{"claude", "--resume", sel.ID}
	if err := syscall.Exec(claudePath, args, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "csm: exec failed: %v\n", err)
		os.Exit(1)
	}
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
