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

// runDownload handles `csm download [options]` — copies every session JSONL
// (subject to filters) into a directory tree or a zip archive.
func runDownload(args []string) int {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	out := fs.String("o", "", "output path; directory by default, zip when --zip")
	zipOut := fs.Bool("zip", false, "package the export as a single .zip archive")
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

	// Apply filters.
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

	if *zipOut {
		return downloadZip(filtered, *out)
	}
	return downloadDir(filtered, *out)
}

func resolveDownloadDir(out string) (string, error) {
	if out != "" {
		return out, nil
	}
	base, err := defaultDownloadsDir()
	if err != nil {
		return "", err
	}
	// Bulk download creates many files — drop them into a dated subfolder
	// under ~/Downloads/ so repeat invocations don't pile into one bin.
	return filepath.Join(base, "csm-"+time.Now().Format("2006-01-02")), nil
}

// downloadDir writes the directory layout:
//   <out>/<project>/<auto-name>.jsonl   per session (verbatim copy)
//   <out>/_index.md                     navigable TOC (markdown)
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
		if hasSubagentDir(s.Path) {
			bundle := filepath.Join(projectDir, exportBundleName(s))
			if err := os.MkdirAll(bundle, 0o755); err != nil {
				fmt.Fprintf(os.Stderr, "csm download: mkdir %s: %v\n", bundle, err)
				continue
			}
			if err := copyExportFile(s.Path, filepath.Join(bundle, filepath.Base(s.Path))); err != nil {
				fmt.Fprintf(os.Stderr, "csm download: export %s: %v\n", s.ID, err)
				continue
			}
			uuid := strings.TrimSuffix(filepath.Base(s.Path), ".jsonl")
			src := filepath.Join(filepath.Dir(s.Path), uuid)
			dst := filepath.Join(bundle, uuid)
			if err := copyDir(src, dst); err != nil {
				fmt.Fprintf(os.Stderr, "csm download: export subagents for %s: %v\n", s.ID, err)
			}
			continue
		}
		path := filepath.Join(projectDir, exportFileName(s))
		f, err := os.Create(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "csm download: create %s: %v\n", path, err)
			continue
		}
		if err := CopySession(f, s); err != nil {
			fmt.Fprintf(os.Stderr, "csm download: export %s: %v\n", s.ID, err)
			f.Close()
			continue
		}
		f.Close()
	}

	// index — markdown TOC linking to per-session JSONL files
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

func downloadZip(sessions []Session, out string) int {
	if out == "" {
		base, err := defaultDownloadsDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, T("export.failed")+"\n", err)
			return 1
		}
		out = filepath.Join(base, "csm-"+time.Now().Format("2006-01-02")+".zip")
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
		if hasSubagentDir(s.Path) {
			if err := writeBundleToZip(w, s); err != nil {
				fmt.Fprintf(os.Stderr, "csm download: zip %s: %v\n", s.ID, err)
			}
			continue
		}
		name := filepath.Join(slug(s.Project), exportFileName(s))
		entry, err := w.Create(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "csm download: zip entry %s: %v\n", name, err)
			continue
		}
		if err := CopySession(entry, s); err != nil {
			fmt.Fprintf(os.Stderr, "csm download: export %s: %v\n", s.ID, err)
			continue
		}
	}
	if entry, err := w.Create("_index.md"); err == nil {
		writeIndex(entry, sessions, true)
	}

	fmt.Fprintf(os.Stderr, T("download.summary")+"\n", len(sessions), out)
	return 0
}

// writeBundleToZip writes a session's jsonl + sibling subagents tree into
// the zip archive under <project>/<bundle>/. Mirrors the on-disk layout so
// the archive is round-trippable.
func writeBundleToZip(w *zip.Writer, s Session) error {
	base := filepath.Join(slug(s.Project), exportBundleName(s))
	mainEntry, err := w.Create(filepath.Join(base, filepath.Base(s.Path)))
	if err != nil {
		return err
	}
	if err := CopySession(mainEntry, s); err != nil {
		return err
	}
	uuid := strings.TrimSuffix(filepath.Base(s.Path), ".jsonl")
	srcDir := filepath.Join(filepath.Dir(s.Path), uuid)
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		entry, err := w.Create(filepath.Join(base, uuid, rel))
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(entry, f)
		return err
	})
}

// writeIndex writes a markdown TOC linking to per-session JSONL files.
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
				var rel string
				if hasSubagentDir(s.Path) {
					rel = filepath.Join(slug(p), exportBundleName(s)) + "/"
				} else {
					rel = filepath.Join(slug(p), exportFileName(s))
				}
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
