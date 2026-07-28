package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// defaultPruneDays is the age threshold used when `csm prune` runs with no
// <days> argument and no --before date.
const defaultPruneDays = 15

// runPrune handles `csm prune [days] [flags]` — bulk-move sessions older than
// the threshold to the trash (or permanently delete with --permanent).
//
// The threshold is relative by default (<days>, 15 when omitted) or absolute
// with --before YYYY-MM-DD. Defaults are safety-first: pinned sessions are
// protected, a preview shows what's about to disappear, and the operation
// requires explicit confirmation. --force / -y / --dry-run cover automation
// and rehearsal needs.
func runPrune(args []string) int {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "preview what would be pruned without changing anything")
	force := fs.Bool("y", false, "skip confirmation prompt (also: --force)")
	forceLong := fs.Bool("force", false, "skip confirmation prompt")
	permanent := fs.Bool("permanent", false, "delete forever instead of moving to trash")
	includePinned := fs.Bool("include-pinned", false, "include pinned sessions (★) — off by default")
	project := fs.String("project", "", "limit to sessions in this project (matches Session.Project name)")
	before := fs.String("before", "", "prune sessions last active before this date (YYYY-MM-DD)")
	fs.SetOutput(os.Stderr)
	// Go's flag.Parse stops at the first positional arg, so flags written
	// AFTER <days> would be silently ignored. Reorder so flags come first
	// and users can write either "prune --dry-run 7" or "prune 7 --dry-run".
	args = reorderFlagsFirst(args, map[string]bool{"project": true, "before": true})
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *before != "" && fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, T("prune.usage_days_and_before"))
		return 2
	}

	var cutoff time.Time
	var threshold string // human label for the threshold, used in messages
	if *before != "" {
		d, err := time.ParseInLocation("2006-01-02", *before, time.Local)
		if err != nil {
			fmt.Fprintf(os.Stderr, T("prune.usage_bad_before")+"\n", *before)
			return 2
		}
		cutoff = d
		threshold = d.Format("2006-01-02")
	} else {
		days := defaultPruneDays
		if fs.NArg() > 0 {
			n, err := strconv.Atoi(fs.Arg(0))
			if err != nil || n < 0 {
				fmt.Fprintf(os.Stderr, T("prune.usage_bad_days")+"\n", fs.Arg(0))
				return 2
			}
			days = n
		}
		cutoff = time.Now().AddDate(0, 0, -days)
		threshold = fmt.Sprintf(T("prune.threshold_days"), days)
	}
	skipConfirm := *force || *forceLong

	sessions, err := LoadSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, T("msg.load_failed")+"\n", err)
		return 1
	}
	pins, _ := LoadPins()
	pinSet := pins.idSet()

	var targets []Session
	for _, s := range sessions {
		if s.LastActivity.IsZero() || !s.LastActivity.Before(cutoff) {
			continue
		}
		if !*includePinned {
			if _, pinned := pinSet[s.ID]; pinned {
				continue
			}
		}
		if *project != "" && !strings.EqualFold(s.Project, *project) {
			continue
		}
		targets = append(targets, s)
	}

	if len(targets) == 0 {
		fmt.Fprintf(os.Stderr, T("prune.none")+"\n", threshold)
		return 0
	}

	// Preview block — sorted oldest-first so users see what's going.
	sort.SliceStable(targets, func(i, j int) bool {
		return targets[i].LastActivity.Before(targets[j].LastActivity)
	})
	printPrunePreview(os.Stderr, targets, threshold, *permanent)

	if *dryRun {
		fmt.Fprintln(os.Stderr, T("prune.dry_run_done"))
		return 0
	}

	if !skipConfirm {
		if !promptYN(T("prune.confirm")) {
			fmt.Fprintln(os.Stderr, T("prune.cancelled"))
			return 0
		}
	}

	moved, failed := 0, 0
	for _, s := range targets {
		var err error
		if *permanent {
			err = PermanentlyDelete(s.Path)
		} else {
			_, err = MoveToTrash(s)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "csm prune: %s: %v\n", s.ID, err)
			failed++
			continue
		}
		moved++
	}

	if *permanent {
		fmt.Fprintf(os.Stderr, T("prune.done_permanent")+"\n", moved)
	} else {
		fmt.Fprintf(os.Stderr, T("prune.done_trash")+"\n", moved)
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, T("prune.partial_fail")+"\n", failed)
		return 1
	}
	return 0
}

func printPrunePreview(w *os.File, targets []Session, threshold string, permanent bool) {
	byProject := map[string]int{}
	for _, s := range targets {
		byProject[s.Project]++
	}
	type proj struct {
		name string
		n    int
	}
	projs := make([]proj, 0, len(byProject))
	for n, c := range byProject {
		projs = append(projs, proj{n, c})
	}
	sort.SliceStable(projs, func(i, j int) bool { return projs[i].n > projs[j].n })
	parts := make([]string, 0, len(projs))
	for _, p := range projs {
		parts = append(parts, fmt.Sprintf("%s (%d)", p.name, p.n))
	}
	dest := T("prune.dest_trash")
	if permanent {
		dest = T("prune.dest_permanent")
	}
	fmt.Fprintf(w, T("prune.preview_header")+"\n", len(targets), threshold, dest)
	fmt.Fprintf(w, T("prune.preview_range")+"\n",
		targets[0].LastActivity.Format("2006-01-02"),
		targets[len(targets)-1].LastActivity.Format("2006-01-02"))
	fmt.Fprintf(w, T("prune.preview_projects")+"\n", strings.Join(parts, ", "))
}

// reorderFlagsFirst returns args with all flag tokens moved before any
// positional ones. valueFlags lists the names of flags that take a value
// in "--flag VAL" form (without =) so the helper keeps the pair together.
// Boolean flags and "--flag=VAL" forms need no special handling.
func reorderFlagsFirst(args []string, valueFlags map[string]bool) []string {
	flags := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		if strings.Contains(a, "=") {
			continue
		}
		name := strings.TrimLeft(a, "-")
		if valueFlags[name] && i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, positional...)
}

// promptYN reads a single y/n line from stdin. Anything but y/yes is a No.
func promptYN(prompt string) bool {
	fmt.Fprint(os.Stderr, prompt+" ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	}
	return false
}
