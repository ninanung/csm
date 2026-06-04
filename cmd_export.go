package main

import (
	"flag"
	"fmt"
	"os"
)

// runExport handles `csm export <session-id> [-o file|-]`. Always emits the
// session's original JSONL bytes verbatim (no markdown conversion).
func runExport(args []string) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	out := fs.String("o", "", "output path; '-' for stdout, empty for default ~/Documents/csm-exports/<auto-name>.jsonl")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "csm export: missing <session-id>")
		fmt.Fprintln(os.Stderr, "usage: csm export <session-id> [-o file|-]")
		return 2
	}
	id := fs.Arg(0)

	sessions, err := LoadSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, T("msg.load_failed")+"\n", err)
		return 1
	}
	var sel *Session
	for i := range sessions {
		if sessions[i].ID == id {
			sel = &sessions[i]
			break
		}
	}
	if sel == nil {
		fmt.Fprintf(os.Stderr, "csm export: session %q not found\n", id)
		return 1
	}

	switch *out {
	case "-":
		if err := CopySession(os.Stdout, *sel); err != nil {
			fmt.Fprintf(os.Stderr, T("export.failed")+"\n", err)
			return 1
		}
	case "":
		path, err := ExportSessionToFile(*sel, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, T("export.failed")+"\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, T("export.success")+"\n", path)
	default:
		f, err := os.Create(*out)
		if err != nil {
			fmt.Fprintf(os.Stderr, T("export.failed")+"\n", err)
			return 1
		}
		defer f.Close()
		if err := CopySession(f, *sel); err != nil {
			fmt.Fprintf(os.Stderr, T("export.failed")+"\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, T("export.success")+"\n", *out)
	}
	return 0
}
