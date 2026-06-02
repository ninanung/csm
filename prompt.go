package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// branchChoice represents the user's answer to "the session's branch doesn't exist".
//   Action == "stay": continue on the current branch (no checkout).
//   Action == "checkout": checkout Branch.
//   Action == "abort": exit without launching claude.
type branchChoice struct {
	Action string
	Branch string
}

// promptMissingBranch asks the user what to do when the recorded session branch
// is not present locally. It writes to stderr and reads a single keystroke (line)
// from stdin. Safe to call after the bubbletea TUI has exited — the terminal is
// back to cooked mode by then.
func promptMissingBranch(missing, current string, available []string) branchChoice {
	r := bufio.NewReader(os.Stdin)

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "csm: branch %q not found locally\n", missing)
	fmt.Fprintf(os.Stderr, "     current: %s\n\n", current)
	fmt.Fprintln(os.Stderr, "  [s] stay on current branch (default)")
	if len(available) > 0 {
		fmt.Fprintln(os.Stderr, "  [p] pick from existing local branches")
	}
	fmt.Fprintln(os.Stderr, "  [a] abort — do not start claude")
	fmt.Fprint(os.Stderr, "> ")

	line, _ := r.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "", "s":
		return branchChoice{Action: "stay"}
	case "a":
		return branchChoice{Action: "abort"}
	case "p":
		if len(available) == 0 {
			fmt.Fprintln(os.Stderr, "no local branches available; staying on current")
			return branchChoice{Action: "stay"}
		}
		return pickBranchFromList(r, available, current)
	default:
		fmt.Fprintf(os.Stderr, "unrecognized choice %q — staying on current\n", strings.TrimSpace(line))
		return branchChoice{Action: "stay"}
	}
}

func pickBranchFromList(r *bufio.Reader, branches []string, current string) branchChoice {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "local branches (most recent first):")
	for i, b := range branches {
		marker := ""
		if b == current {
			marker = "  ← current"
		}
		fmt.Fprintf(os.Stderr, "  %2d. %s%s\n", i+1, b, marker)
	}
	fmt.Fprint(os.Stderr, "\nnumber (blank to cancel): ")

	line, _ := r.ReadString('\n')
	in := strings.TrimSpace(line)
	if in == "" {
		return branchChoice{Action: "stay"}
	}
	n, err := strconv.Atoi(in)
	if err != nil || n < 1 || n > len(branches) {
		fmt.Fprintf(os.Stderr, "invalid selection %q — staying on current\n", in)
		return branchChoice{Action: "stay"}
	}
	picked := branches[n-1]
	if picked == current {
		return branchChoice{Action: "stay"}
	}
	return branchChoice{Action: "checkout", Branch: picked}
}
