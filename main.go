package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

const usage = `csm — Claude Code session manager

Usage:
  csm                Interactive picker; on select, exec 'claude --resume <id>' in
                     the selected session's cwd. Replaces this process.
  csm --print        Interactive picker; print "<session-id>\t<cwd>" to stdout
                     and exit. For multiplexer adapters.
  csm --list-json    Print all sessions as JSON array to stdout and exit.
                     For programmatic use (e.g., the csm skill).
  csm install        Install the global Claude skill at ~/.claude/skills/csm/.
  csm install --force
                     Overwrite an existing skill.
  csm uninstall      Remove the installed skill.
  csm -h             Show this help.

Keys (interactive mode):
  ↑/↓ or j/k         navigate
  enter              select
  /                  filter (esc to cancel)
  g/G                jump to first/last session
  q                  quit
`

func main() {
	// Subcommands handled before flag parsing.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			force := len(os.Args) > 2 && os.Args[2] == "--force"
			if err := installSkill(force); err != nil {
				fmt.Fprintf(os.Stderr, "csm install: %v\n", err)
				os.Exit(1)
			}
			return
		case "uninstall":
			if err := uninstallSkill(); err != nil {
				fmt.Fprintf(os.Stderr, "csm uninstall: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	printMode := flag.Bool("print", false, "print selection to stdout instead of exec'ing claude")
	listJSON := flag.Bool("list-json", false, "print all sessions as JSON and exit (non-interactive)")
	flag.Usage = func() { fmt.Print(usage) }
	flag.Parse()

	sessions, err := LoadSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "csm: failed to load sessions: %v\n", err)
		os.Exit(1)
	}

	if *listJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(sessions); err != nil {
			fmt.Fprintf(os.Stderr, "csm: encode json: %v\n", err)
			os.Exit(1)
		}
		return
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
