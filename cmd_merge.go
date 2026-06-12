package main

import (
	"flag"
	"fmt"
	"os"
)

// runMerge handles `csm merge <id> <id> [<id>…]`. Stitches the given sessions
// into one new resumable session and prints its new id. The interactive
// equivalent is TUI marking (space) + m.
func runMerge(args []string) int {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, T("merge.need_two"))
		fmt.Fprintln(os.Stderr, "usage: csm merge <session-id> <session-id> [<session-id>…]")
		return 2
	}

	sessions, err := LoadSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, T("msg.load_failed")+"\n", err)
		return 1
	}

	var selected []Session
	for _, id := range fs.Args() {
		var found *Session
		for i := range sessions {
			if sessions[i].ID == id {
				found = &sessions[i]
				break
			}
		}
		if found == nil {
			fmt.Fprintf(os.Stderr, T("merge.not_found")+"\n", id)
			return 1
		}
		selected = append(selected, *found)
	}

	fmt.Fprintln(os.Stderr, T("merge.running"))
	targetID, folded, err := MergeConsolidate(selected)
	if err != nil {
		fmt.Fprintf(os.Stderr, T("merge.failed")+"\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, T("merge.success_cli")+"\n", targetID, folded)
	fmt.Fprintln(os.Stdout, targetID) // stdout: target id, for scripting
	return 0
}
