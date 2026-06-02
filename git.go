package main

import (
	"os"
	"os/exec"
	"strings"
)

type GitState struct {
	CWDExists       bool
	IsRepo          bool
	CurrentBranch   string
	BranchExists    bool // session's branch exists locally
	BranchAtCWD     bool // session's branch currently checked out at cwd
	BranchInWorktree string // path of another worktree holding the branch, if any
	Dirty           bool
	InProgress      string // "rebase", "merge", "cherry-pick", or ""
}

// CheckGitState inspects the working state at the session's cwd relative to its branch.
func CheckGitState(cwd, sessionBranch string) GitState {
	st := GitState{}
	if cwd == "" {
		return st
	}
	if info, err := os.Stat(cwd); err == nil && info.IsDir() {
		st.CWDExists = true
	} else {
		return st
	}

	// is it a git repo?
	if out, err := gitAt(cwd, "rev-parse", "--is-inside-work-tree"); err != nil || strings.TrimSpace(out) != "true" {
		return st
	}
	st.IsRepo = true

	// current branch (or HEAD sha for detached)
	if out, err := gitAt(cwd, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		st.CurrentBranch = strings.TrimSpace(out)
	}

	if sessionBranch != "" {
		st.BranchAtCWD = st.CurrentBranch == sessionBranch
		// branch existence: try show-ref
		if _, err := gitAt(cwd, "show-ref", "--verify", "--quiet", "refs/heads/"+sessionBranch); err == nil {
			st.BranchExists = true
		}
		// is it checked out at another worktree?
		if !st.BranchAtCWD && st.BranchExists {
			if out, err := gitAt(cwd, "worktree", "list", "--porcelain"); err == nil {
				st.BranchInWorktree = findWorktreeForBranch(out, sessionBranch)
			}
		}
	}

	// dirty
	if out, err := gitAt(cwd, "status", "--porcelain"); err == nil && strings.TrimSpace(out) != "" {
		st.Dirty = true
	}

	// in-progress operations
	gitDir, _ := gitAt(cwd, "rev-parse", "--git-dir")
	gitDir = strings.TrimSpace(gitDir)
	if gitDir != "" {
		for _, marker := range []struct {
			file string
			name string
		}{
			{"rebase-merge", "rebase"},
			{"rebase-apply", "rebase"},
			{"MERGE_HEAD", "merge"},
			{"CHERRY_PICK_HEAD", "cherry-pick"},
		} {
			p := gitDir + "/" + marker.file
			if _, err := os.Stat(p); err == nil {
				st.InProgress = marker.name
				break
			}
		}
	}

	return st
}

func gitAt(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	return string(out), err
}

func findWorktreeForBranch(porcelain, branch string) string {
	var currentPath string
	for _, line := range strings.Split(porcelain, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			currentPath = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch refs/heads/"):
			if strings.TrimPrefix(line, "branch refs/heads/") == branch {
				return currentPath
			}
		}
	}
	return ""
}
