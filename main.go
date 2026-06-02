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
  csm           Interactive picker; on select, cd into the session's cwd,
                align the git branch (when safe), and exec 'claude --resume <id>'.
                Replaces this process.
  csm --print   Interactive picker; print "<session-id>\t<cwd>" to stdout
                and exit. For external adapters/scripts.
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

	// Default mode: cd + (optional) branch checkout + exec claude --resume.
	target := sel.CWD
	if target == "" || !dirExists(target) {
		fmt.Fprintf(os.Stderr, "csm: session cwd missing or absent (%q). starting in current dir.\n", target)
		target = cwd
	}
	if err := os.Chdir(target); err != nil {
		fmt.Fprintf(os.Stderr, "csm: chdir failed: %v\n", err)
		os.Exit(1)
	}

	// Align git branch with the session when safe. Skip when:
	// - session has no recorded branch
	// - we're already on it
	// - the branch doesn't exist locally
	// - working tree is dirty (would risk losing changes)
	// - a rebase/merge/cherry-pick is in progress
	// - the branch is checked out at another worktree
	// In any unsafe case we warn but proceed without switching.
	if sel.GitBranch != "" {
		state := CheckGitState(target, sel.GitBranch)
		switch {
		case !state.IsRepo:
			// nothing to do
		case state.BranchAtCWD:
			// already aligned
		case !state.BranchExists:
			fmt.Fprintf(os.Stderr, "csm: branch %q not found locally; staying on %q\n", sel.GitBranch, state.CurrentBranch)
		case state.Dirty:
			fmt.Fprintf(os.Stderr, "csm: working tree dirty; staying on %q (session was on %q)\n", state.CurrentBranch, sel.GitBranch)
		case state.InProgress != "":
			fmt.Fprintf(os.Stderr, "csm: %s in progress; staying on %q\n", state.InProgress, state.CurrentBranch)
		case state.BranchInWorktree != "":
			fmt.Fprintf(os.Stderr, "csm: branch %q is checked out at %s; staying on %q\n", sel.GitBranch, state.BranchInWorktree, state.CurrentBranch)
		default:
			out, err := exec.Command("git", "-C", target, "checkout", sel.GitBranch).CombinedOutput()
			if err != nil {
				fmt.Fprintf(os.Stderr, "csm: git checkout %q failed: %v\n%s", sel.GitBranch, err, out)
			} else {
				fmt.Fprintf(os.Stderr, "csm: switched to branch %q\n", sel.GitBranch)
			}
		}
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
