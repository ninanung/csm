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
  csm --lang    Force interface language: 'en' or 'ko'.
                Default: detected from CSM_LANG / LC_ALL / LANG.
  csm version   Show ASCII logo, version, and command summary.
  csm -h        Show this help.

Keys:
  ↑/↓ or j/k    navigate
  enter         select
  /             filter (esc to cancel)
  g/G           jump to first/last session
  q             quit
`

func main() {
	// `csm version` subcommand — handled before flag.Parse so positional arg works.
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version" || os.Args[1] == "-v") {
		printSplash(os.Stdout)
		return
	}

	printMode := flag.Bool("print", false, "print selection to stdout instead of exec'ing claude")
	langFlag := flag.String("lang", "", "force language: 'en' or 'ko' (overrides CSM_LANG/LANG)")
	flag.Usage = func() { fmt.Print(usage) }
	flag.Parse()

	if *langFlag != "" {
		SetLang(*langFlag)
	}

	sessions, err := LoadSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, T("msg.load_failed")+"\n", err)
		os.Exit(1)
	}
	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, T("msg.no_sessions"))
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
		fmt.Fprintf(os.Stderr, T("msg.cwd_missing")+"\n", target)
		target = cwd
	}
	if err := os.Chdir(target); err != nil {
		fmt.Fprintf(os.Stderr, T("msg.chdir_failed")+"\n", err)
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
			branches, _ := ListLocalBranches(target)
			ch := promptMissingBranch(sel.GitBranch, state.CurrentBranch, branches)
			switch ch.Action {
			case "abort":
				fmt.Fprintln(os.Stderr, T("msg.aborted"))
				os.Exit(0)
			case "checkout":
				out, err := exec.Command("git", "-C", target, "checkout", ch.Branch).CombinedOutput()
				if err != nil {
					fmt.Fprintf(os.Stderr, T("msg.checkout_failed"), ch.Branch, err, out)
				} else {
					fmt.Fprintf(os.Stderr, T("msg.switched")+"\n", ch.Branch)
				}
			default:
				// stay — proceed without switching
			}
		case state.Dirty:
			fmt.Fprintf(os.Stderr, T("msg.staying_dirty")+"\n", state.CurrentBranch, sel.GitBranch)
		case state.InProgress != "":
			fmt.Fprintf(os.Stderr, T("msg.in_progress")+"\n", state.InProgress, state.CurrentBranch)
		case state.BranchInWorktree != "":
			fmt.Fprintf(os.Stderr, T("msg.branch_in_worktree")+"\n", sel.GitBranch, state.BranchInWorktree, state.CurrentBranch)
		default:
			out, err := exec.Command("git", "-C", target, "checkout", sel.GitBranch).CombinedOutput()
			if err != nil {
				fmt.Fprintf(os.Stderr, T("msg.checkout_failed"), sel.GitBranch, err, out)
			} else {
				fmt.Fprintf(os.Stderr, T("msg.switched")+"\n", sel.GitBranch)
			}
		}
	}

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintf(os.Stderr, T("msg.claude_missing")+"\n", err)
		os.Exit(1)
	}
	args := []string{"claude", "--resume", sel.ID}
	if err := syscall.Exec(claudePath, args, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, T("msg.exec_failed")+"\n", err)
		os.Exit(1)
	}
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
