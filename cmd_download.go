package main

import (
	"archive/zip"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// runDownload handles `csm download [options]` — bulk-export every session
// (subject to filters) into a directory tree, a zip file, or a single combined
// markdown file.
func runDownload(args []string) int {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	out := fs.String("o", "", "output path; directory by default, zip when --zip, file when --single-file")
	zipOut := fs.Bool("zip", false, "package the export as a single .zip archive")
	single := fs.Bool("single-file", false, "concatenate all sessions into one markdown file")
	since := fs.String("since", "", "include only sessions active on or after this date (YYYY-MM-DD)")
	project := fs.String("project", "", "include only sessions whose project matches this name")
	minMsgs := fs.Int("min-msgs", 0, "exclude sessions with fewer than N messages")
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	sessions, err := LoadSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, T("msg.load_failed")+"\n", err)
		return 1
	}

	// Apply filters in-place.
	filtered := sessions[:0]
	var sinceTime time.Time
	if *since != "" {
		t, err := time.Parse("2006-01-02", *since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "csm download: invalid --since %q (want YYYY-MM-DD)\n", *since)
			return 2
		}
		sinceTime = t
	}
	for _, s := range sessions {
		if !sinceTime.IsZero() && s.LastActivity.Before(sinceTime) {
			continue
		}
		if *project != "" && !strings.EqualFold(s.Project, *project) {
			continue
		}
		if *minMsgs > 0 && s.MessageCount < *minMsgs {
			continue
		}
		filtered = append(filtered, s)
	}
	if len(filtered) == 0 {
		fmt.Fprintln(os.Stderr, "csm download: no sessions match the given filters")
		return 1
	}

	switch {
	case *single:
		return downloadSingleFile(filtered, *out)
	case *zipOut:
		return downloadZip(filtered, *out)
	default:
		return downloadDir(filtered, *out)
	}
}

func resolveDownloadDir(out string) (string, error) {
	if out != "" {
		return out, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Documents", "csm-downloads"), nil
}

// downloadDir writes the directory tree layout:
//   <out>/<project>/<auto-name>.md   per session
//   <out>/_index.md                  table of contents
func downloadDir(sessions []Session, out string) int {
	dir, err := resolveDownloadDir(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, T("export.failed")+"\n", err)
		return 1
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, T("export.failed")+"\n", err)
		return 1
	}
	for _, s := range sessions {
		projectDir := filepath.Join(dir, slug(s.Project))
		if err := os.MkdirAll(projectDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "csm download: mkdir %s: %v\n", projectDir, err)
			continue
		}
		path := filepath.Join(projectDir, exportFileName(s))
		f, err := os.Create(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "csm download: create %s: %v\n", path, err)
			continue
		}
		if err := ExportSession(f, s, defaultExportOptions()); err != nil {
			fmt.Fprintf(os.Stderr, "csm download: export %s: %v\n", s.ID, err)
			f.Close()
			continue
		}
		f.Close()
	}

	// index
	fmt.Fprintln(os.Stderr, T("download.indexing"))
	indexPath := filepath.Join(dir, "_index.md")
	idx, err := os.Create(indexPath)
	if err == nil {
		writeIndex(idx, sessions, true)
		idx.Close()
	}

	fmt.Fprintf(os.Stderr, T("download.summary")+"\n", len(sessions), dir)
	return 0
}

// downloadZip writes the same directory tree into a .zip archive at out (or
// ~/Documents/csm-downloads/csm-<date>.zip when out is empty).
func downloadZip(sessions []Session, out string) int {
	if out == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, T("export.failed")+"\n", err)
			return 1
		}
		dir := filepath.Join(home, "Documents", "csm-downloads")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, T("export.failed")+"\n", err)
			return 1
		}
		out = filepath.Join(dir, "csm-"+time.Now().Format("2006-01-02")+".zip")
	}
	zf, err := os.Create(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, T("export.failed")+"\n", err)
		return 1
	}
	defer zf.Close()
	w := zip.NewWriter(zf)
	defer w.Close()

	for _, s := range sessions {
		name := filepath.Join(slug(s.Project), exportFileName(s))
		entry, err := w.Create(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "csm download: zip entry %s: %v\n", name, err)
			continue
		}
		if err := ExportSession(entry, s, defaultExportOptions()); err != nil {
			fmt.Fprintf(os.Stderr, "csm download: export %s: %v\n", s.ID, err)
			continue
		}
	}
	// _index.md
	if entry, err := w.Create("_index.md"); err == nil {
		writeIndex(entry, sessions, true)
	}

	fmt.Fprintf(os.Stderr, T("download.summary")+"\n", len(sessions), out)
	return 0
}

// downloadSingleFile concatenates the index plus every session into a single
// markdown file at out (or ~/Documents/csm-downloads/csm-<date>.md by default).
func downloadSingleFile(sessions []Session, out string) int {
	if out == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, T("export.failed")+"\n", err)
			return 1
		}
		dir := filepath.Join(home, "Documents", "csm-downloads")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, T("export.failed")+"\n", err)
			return 1
		}
		out = filepath.Join(dir, "csm-"+time.Now().Format("2006-01-02")+".md")
	}
	f, err := os.Create(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, T("export.failed")+"\n", err)
		return 1
	}
	defer f.Close()

	writeIndex(f, sessions, false)
	for _, s := range sessions {
		fmt.Fprintln(f)
		fmt.Fprintln(f, "---")
		fmt.Fprintln(f)
		if err := ExportSession(f, s, defaultExportOptions()); err != nil {
			fmt.Fprintf(os.Stderr, "csm download: export %s: %v\n", s.ID, err)
		}
	}
	fmt.Fprintf(os.Stderr, T("download.summary")+"\n", len(sessions), out)
	return 0
}

// writeIndex writes a markdown table of contents for sessions. When withLinks
// is true, each row links to the per-session file (relative path).
func writeIndex(w io.Writer, sessions []Session, withLinks bool) {
	fmt.Fprintf(w, "# csm sessions  (%d sessions", len(sessions))
	var oldest, newest time.Time
	for _, s := range sessions {
		if oldest.IsZero() || s.LastActivity.Before(oldest) {
			oldest = s.LastActivity
		}
		if s.LastActivity.After(newest) {
			newest = s.LastActivity
		}
	}
	if !oldest.IsZero() && !newest.IsZero() {
		fmt.Fprintf(w, ", %s ~ %s", oldest.Format("2006-01-02"), newest.Format("2006-01-02"))
	}
	fmt.Fprintln(w, ")")
	fmt.Fprintln(w)

	byProject := map[string][]Session{}
	for _, s := range sessions {
		byProject[s.Project] = append(byProject[s.Project], s)
	}
	projectNames := make([]string, 0, len(byProject))
	for p := range byProject {
		projectNames = append(projectNames, p)
	}
	sort.Strings(projectNames)

	for _, p := range projectNames {
		group := byProject[p]
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].LastActivity.After(group[j].LastActivity)
		})
		fmt.Fprintf(w, "## %s (%d)\n\n", p, len(group))
		fmt.Fprintln(w, "| Date | Title | Branch | Msgs |")
		fmt.Fprintln(w, "|---|---|---|---|")
		for _, s := range group {
			date := "—"
			if !s.LastActivity.IsZero() {
				date = s.LastActivity.Format("2006-01-02")
			}
			title := oneLine(s.FirstMessage)
			if title == "" {
				title = "(no message)"
			}
			if withLinks {
				rel := filepath.Join(slug(p), exportFileName(s))
				title = fmt.Sprintf("[%s](%s)", title, rel)
			}
			branch := s.GitBranch
			if branch == "" {
				branch = "—"
			}
			fmt.Fprintf(w, "| %s | %s | %s | %d |\n", date, title, branch, s.MessageCount)
		}
		fmt.Fprintln(w)
	}
}
