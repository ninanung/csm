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
  csm completion <bash|zsh|fish>
                Print shell completion script to stdout.
  csm export <session-id> [-o file|-]
                Export the session's raw JSONL bytes verbatim.
                Default output: ~/Downloads/<auto-name>.jsonl
  csm download [-o path] [--zip]
                [--since YYYY-MM-DD] [--project NAME] [--min-msgs N]
                Bulk-export sessions as JSONL.
                Default directory output: ~/Downloads/csm-<date>/
                Default zip output:       ~/Downloads/csm-<date>.zip
  csm -h        Show this help.

Keys:
  ↑/↓ or j/k    navigate
  enter         select
  /             filter (esc to cancel)
  g/G           jump to first/last session
  q             quit
`

func main() {
	// Subcommands handled before flag.Parse so positional args work.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			printSplash(os.Stdout)
			return
		case "completion":
			shell := ""
			if len(os.Args) > 2 {
				shell = os.Args[2]
			}
			os.Exit(printCompletion(os.Stdout, shell))
		case "export":
			os.Exit(runExport(os.Args[2:]))
		case "download":
			os.Exit(runDownload(os.Args[2:]))
		case "cleanup":
			n, err := CleanupOrphanSubagentDirs()
			if err != nil {
				fmt.Fprintf(os.Stderr, "csm: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintf(os.Stdout, T("trash.cleanup_orphans")+"\n", n)
			return
		}
	}

	printMode := flag.Bool("print", false, "print selection to stdout instead of exec'ing claude")
	langFlag := flag.String("lang", "", "force language: 'en' or 'ko' (overrides CSM_LANG/LANG)")
	flag.Usage = func() { fmt.Print(usage) }
	flag.Parse()

	if *langFlag != "" {
		SetLang(*langFlag)
	}

	// preflight: environment checks before TUI launch. Renders a friendly
	// empty-state when something's missing instead of a terse error.
	if reason, ok := preflight(); !ok {
		printEmptyState(os.Stderr, reason)
		os.Exit(0)
	}

	// One-shot cleanup of orphan sub-agent dirs left behind by earlier csm
	// versions that only trashed the main jsonl. Silent on the happy path.
	if n, err := CleanupOrphanSubagentDirs(); err == nil && n > 0 {
		fmt.Fprintf(os.Stderr, T("trash.cleanup_orphans")+"\n", n)
	}

	sessions, err := LoadSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, T("msg.load_failed")+"\n", err)
		os.Exit(1)
	}
	if len(sessions) == 0 {
		printEmptyState(os.Stderr, emptyNoSessions)
		os.Exit(0)
	}

	cwd, _ := os.Getwd()
	m := NewModel(sessions, cwd)
	prog := tea.NewProgram(m, tea.WithOutput(os.Stderr), tea.WithMouseCellMotion())
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
