package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Session struct {
	ID           string
	Path         string
	CWD          string
	GitBranch    string
	FirstMessage string
	LastActivity time.Time
	// Size is the jsonl size in bytes. The picker shows it instead of a
	// message count, which would need a full read of the file.
	Size int64
	// MessageCount is filled only by countMessages (csm download). The
	// head/tail loader leaves it 0.
	MessageCount int
	Project      string
	// Entrypoint reflects how Claude Code was launched for this session.
	// "cli" = user-started terminal session (default).
	// "sdk-cli" = SDK / orchestration tool spawn (worktree agents, etc.).
	// Empty when the JSONL has no entrypoint field.
	Entrypoint string
	// SubAgentCount is the number of agent-*.jsonl files under
	// <uuid>/subagents/. Drives the "↳N" badge in session rows and the `s`
	// key's enter-sub-agent-view gate.
	SubAgentCount int
}

const sdkEntrypoint = "sdk-cli"

type rawLine struct {
	Type       string          `json:"type"`
	CWD        string          `json:"cwd"`
	GitBranch  string          `json:"gitBranch"`
	Timestamp  string          `json:"timestamp"`
	Entrypoint string          `json:"entrypoint"`
	Message    json.RawMessage `json:"message"`
}

type rawMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

const (
	maxLineBytes = 16 << 20
	probeWindow  = 32 << 10
	tailWindow   = 256 << 10
)

var (
	entrypointKey  = []byte(`"entrypoint":"`)
	gitBranchKey   = []byte(`"gitBranch":"`)
	emptyBranchKey = []byte(`"gitBranch":""`)
	timestampKey   = []byte(`"timestamp":"`)
)

func projectsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

func newLineScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	// some sessions have very long lines (large pasted content); raise buffer
	sc.Buffer(make([]byte, 0, 64<<10), maxLineBytes)
	return sc
}

// ---------- listing ----------

// sessionFile is a jsonl found by listing a session root — enough to order
// and count sessions without opening them.
type sessionFile struct {
	Path         string
	ID           string
	Dir          string
	Size         int64
	FileMtime    time.Time
	SidecarMtime time.Time
}

// ModTime is the latest write to the session or to its sidecar dir.
func (f sessionFile) ModTime() time.Time {
	if f.SidecarMtime.After(f.FileMtime) {
		return f.SidecarMtime
	}
	return f.FileMtime
}

// listSessionFiles stats every <root>/<project>/<uuid>.jsonl, newest first.
func listSessionFiles(root string) ([]sessionFile, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []sessionFile
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			name := f.Name()
			if f.IsDir() || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			p := filepath.Join(dir, name)
			out = append(out, sessionFile{
				Path:         p,
				ID:           strings.TrimSuffix(name, ".jsonl"),
				Dir:          dir,
				Size:         info.Size(),
				FileMtime:    info.ModTime(),
				SidecarMtime: latestSidecarMtime(p),
			})
		}
	}
	sortFilesNewestFirst(out)
	return out, nil
}

func sortFilesNewestFirst(fs []sessionFile) {
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].ModTime().After(fs[j].ModTime()) })
}

// ---------- reading one session ----------

// loadSessionMeta reads only the head and the tail of a session file, so its
// cost doesn't grow with the length of the conversation.
func loadSessionMeta(sf sessionFile) (Session, error) {
	s := Session{ID: sf.ID, Path: sf.Path, Size: sf.Size}
	f, err := os.Open(sf.Path)
	if err != nil {
		return s, err
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil {
		s.Size = info.Size()
	}

	readHead(f, &s)
	readTail(f, s.Size, &s)

	if s.LastActivity.IsZero() {
		s.LastActivity = sf.FileMtime
	}
	// Sub-agents and tool-results that ran after the main session line was
	// last written should pull LastActivity forward. Otherwise an active
	// session whose orchestration kept spawning sub-agents reads as "stale".
	if sf.SidecarMtime.After(s.LastActivity) {
		s.LastActivity = sf.SidecarMtime
	}
	s.SubAgentCount = countSubAgents(sf.Path)
	s.Project = deriveProject(s.CWD)
	return s, nil
}

// readHead fills the fields written at the start of a session: cwd,
// entrypoint and the first user message.
func readHead(r io.Reader, s *Session) {
	sc := newLineScanner(r)
	for sc.Scan() {
		var l rawLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		if s.CWD == "" && l.CWD != "" {
			s.CWD = l.CWD
		}
		if s.Entrypoint == "" && l.Entrypoint != "" {
			s.Entrypoint = l.Entrypoint
		}
		if s.FirstMessage == "" && l.Type == "user" && len(l.Message) > 0 {
			s.FirstMessage = extractFirstText(l.Message)
		}
		if s.CWD != "" && s.FirstMessage != "" {
			return
		}
	}
}

type tailLine struct {
	GitBranch string `json:"gitBranch"`
	Timestamp string `json:"timestamp"`
}

// readTail fills the fields that change as a session goes on: the last
// non-empty gitBranch (so a mid-session branch switch carries over on resume)
// and the latest timestamp. The window grows from the end of the file until
// both are found or the whole file has been read.
func readTail(f *os.File, size int64, s *Session) {
	win := int64(tailWindow)
	for {
		whole := win >= size
		if whole {
			win = size
		}
		buf := make([]byte, win)
		if _, err := f.ReadAt(buf, size-win); err != nil && err != io.EOF {
			return
		}
		lines := bytes.Split(buf, []byte{'\n'})
		if !whole && len(lines) > 0 {
			lines = lines[1:] // first line is cut by the window
		}
		branch, emptySeen, last := "", false, time.Time{}
		for i := len(lines) - 1; i >= 0; i-- {
			ln := lines[i]
			if !bytes.Contains(ln, timestampKey) && !bytes.Contains(ln, gitBranchKey) {
				continue
			}
			if bytes.Contains(ln, emptyBranchKey) {
				emptySeen = true
			}
			var l tailLine
			if json.Unmarshal(ln, &l) != nil {
				continue
			}
			if branch == "" && l.GitBranch != "" {
				branch = l.GitBranch
			}
			if l.Timestamp != "" {
				if t, err := time.Parse(time.RFC3339, l.Timestamp); err == nil && t.After(last) {
					last = t
				}
			}
		}
		if whole || (!last.IsZero() && (branch != "" || emptySeen)) {
			s.GitBranch = branch
			s.LastActivity = last
			return
		}
		win *= 4
	}
}

// probeEntrypoint finds a session's entrypoint by byte-searching its first and
// last 32KB, with a line scan as the last resort. The picker needs it for every
// session (SDK runs are hidden) but nothing else from most of them.
func probeEntrypoint(path string, buf []byte) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	size := info.Size()

	n, _ := io.ReadFull(f, buf)
	if v, ok := entrypointIn(buf[:n], false); ok {
		return v
	}
	if int64(n) >= size {
		return ""
	}
	if _, err := f.ReadAt(buf, size-int64(len(buf))); err == nil {
		if v, ok := entrypointIn(buf, true); ok {
			return v
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	sc := newLineScanner(f)
	for sc.Scan() {
		var l struct {
			Entrypoint string `json:"entrypoint"`
		}
		if json.Unmarshal(sc.Bytes(), &l) == nil && l.Entrypoint != "" {
			return l.Entrypoint
		}
	}
	return ""
}

func entrypointIn(b []byte, last bool) (string, bool) {
	i := bytes.Index(b, entrypointKey)
	if last {
		i = bytes.LastIndex(b, entrypointKey)
	}
	if i < 0 {
		return "", false
	}
	rest := b[i+len(entrypointKey):]
	j := bytes.IndexByte(rest, '"')
	if j < 0 {
		return "", false
	}
	return string(rest[:j]), true
}

// countMessages counts user/assistant lines. It reads the whole file, so only
// commands that report the count (csm download) call it.
func countMessages(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	sc := newLineScanner(f)
	for sc.Scan() {
		var l struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		if l.Type == "user" || l.Type == "assistant" {
			n++
		}
	}
	return n
}

// ---------- bulk loading ----------

// parallelEach runs fn for 0..n-1 on up to NumCPU goroutines.
func parallelEach(n int, fn func(i int)) {
	workers := runtime.NumCPU()
	if workers > n {
		workers = n
	}
	if workers <= 1 {
		for i := 0; i < n; i++ {
			fn(i)
		}
		return
	}
	var next atomic.Int64
	next.Store(-1)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1))
				if i >= n {
					return
				}
				fn(i)
			}
		}()
	}
	wg.Wait()
}

// loadResult is the outcome of reading a batch of files. It can be built off
// the UI goroutine and applied to a catalog later.
type loadResult struct {
	sessions []Session
	missing  []string // paths that vanished since listing
}

func loadFiles(files []sessionFile) loadResult {
	ss := make([]Session, len(files))
	ok := make([]bool, len(files))
	parallelEach(len(files), func(i int) {
		if s, err := loadSessionMeta(files[i]); err == nil {
			ss[i], ok[i] = s, true
		}
	})
	var res loadResult
	for i := range files {
		if ok[i] {
			res.sessions = append(res.sessions, ss[i])
		} else {
			res.missing = append(res.missing, files[i].Path)
		}
	}
	return res
}

func sortByActivity(ss []Session) {
	sort.SliceStable(ss, func(i, j int) bool {
		return ss[i].LastActivity.After(ss[j].LastActivity)
	})
}

// LoadSessions reads head/tail metadata for every live session, most recent
// first. Bulk CLI commands use it; the picker pages through a catalog instead.
func LoadSessions() ([]Session, error) {
	root, err := projectsRoot()
	if err != nil {
		return nil, err
	}
	files, err := listSessionFiles(root)
	if err != nil {
		return nil, err
	}
	sessions := loadFiles(files).sessions
	sortByActivity(sessions)
	return sessions, nil
}

// findSession loads one live session by id without reading the others.
func findSession(id string) (Session, bool) {
	if id == "" || strings.ContainsAny(id, `/\`) {
		return Session{}, false
	}
	root, err := projectsRoot()
	if err != nil {
		return Session{}, false
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return Session{}, false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(root, e.Name(), id+".jsonl")
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		s, err := loadSessionMeta(sessionFile{
			Path:         p,
			ID:           id,
			Dir:          filepath.Dir(p),
			Size:         info.Size(),
			FileMtime:    info.ModTime(),
			SidecarMtime: latestSidecarMtime(p),
		})
		return s, err == nil
	}
	return Session{}, false
}

// ---------- catalog: lazy loading for the picker ----------

// catalog indexes every session file under one root and reads metadata only
// when a screen needs it: the overview loads a few sessions per project,
// drill-down and search load the rest on demand.
type catalog struct {
	entries    []catalogEntry // newest first
	index      map[string]int // path → entries index
	sessions   []Session      // loaded so far, most recent first
	dirProject map[string]string
	probed     bool
	loading    bool // a background load for search is in flight
}

type catalogEntry struct {
	file       sessionFile
	entrypoint string
	loaded     bool
}

func (e catalogEntry) shown(includeAgents bool) bool {
	return includeAgents || e.entrypoint != sdkEntrypoint
}

var probeBufs = sync.Pool{New: func() any {
	b := make([]byte, probeWindow)
	return &b
}}

// newCatalog lists root. With probe it also reads each file's entrypoint, so
// SDK sessions can be hidden and counted without loading them.
func newCatalog(root string, probe bool) (*catalog, error) {
	files, err := listSessionFiles(root)
	if err != nil {
		return nil, err
	}
	c := &catalog{
		entries:    make([]catalogEntry, len(files)),
		dirProject: map[string]string{},
		probed:     probe,
	}
	for i, f := range files {
		c.entries[i].file = f
	}
	if probe {
		parallelEach(len(c.entries), func(i int) {
			bp := probeBufs.Get().(*[]byte)
			c.entries[i].entrypoint = probeEntrypoint(c.entries[i].file.Path, *bp)
			probeBufs.Put(bp)
		})
	}
	c.reindex()
	return c, nil
}

// openLiveCatalog indexes ~/.claude/projects.
func openLiveCatalog() (*catalog, error) {
	root, err := projectsRoot()
	if err != nil {
		return nil, err
	}
	return newCatalog(root, true)
}

func (c *catalog) reindex() {
	c.index = make(map[string]int, len(c.entries))
	for i, e := range c.entries {
		c.index[e.file.Path] = i
	}
}

func (c *catalog) empty() bool { return len(c.entries) == 0 }

func (c *catalog) load(idx []int) int {
	files := make([]sessionFile, len(idx))
	for k, i := range idx {
		files[k] = c.entries[i].file
	}
	return c.apply(loadFiles(files))
}

// apply adds a load result. Entries removed or loaded in the meantime are
// skipped. Returns how many sessions were added.
func (c *catalog) apply(res loadResult) int {
	n := 0
	for _, s := range res.sessions {
		i, ok := c.index[s.Path]
		if !ok || c.entries[i].loaded {
			continue
		}
		e := &c.entries[i]
		if c.probed {
			s.Entrypoint = e.entrypoint
		}
		e.loaded = true
		if _, ok := c.dirProject[e.file.Dir]; !ok {
			c.dirProject[e.file.Dir] = s.Project
		}
		c.sessions = append(c.sessions, s)
		n++
	}
	for _, p := range res.missing {
		c.remove(p)
	}
	if n > 0 {
		sortByActivity(c.sessions)
	}
	return n
}

// ensureOverview loads the newest perDir shown sessions of every project folder.
func (c *catalog) ensureOverview(perDir int, includeAgents bool) {
	taken := map[string]int{}
	var idx []int
	for i, e := range c.entries {
		if !e.shown(includeAgents) || taken[e.file.Dir] >= perDir {
			continue
		}
		taken[e.file.Dir]++
		if !e.loaded {
			idx = append(idx, i)
		}
	}
	if len(idx) > 0 {
		c.load(idx)
	}
}

// ensureIDs loads specific sessions (pinned ones) wherever they rank.
func (c *catalog) ensureIDs(ids map[string]struct{}) {
	var idx []int
	for i, e := range c.entries {
		if _, ok := ids[e.file.ID]; ok && !e.loaded {
			idx = append(idx, i)
		}
	}
	if len(idx) > 0 {
		c.load(idx)
	}
}

// loadMoreOf loads the next n unread sessions of one project, newest first.
func (c *catalog) loadMoreOf(project string, n int, includeAgents bool) int {
	var idx []int
	for i, e := range c.entries {
		if len(idx) == n {
			break
		}
		if e.loaded || !e.shown(includeAgents) {
			continue
		}
		if p, ok := c.dirProject[e.file.Dir]; !ok || p != project {
			continue
		}
		idx = append(idx, i)
	}
	if len(idx) == 0 {
		return 0
	}
	return c.load(idx)
}

// pending lists the shown files that haven't been read yet.
func (c *catalog) pending(includeAgents bool) []sessionFile {
	var out []sessionFile
	for _, e := range c.entries {
		if !e.loaded && e.shown(includeAgents) {
			out = append(out, e.file)
		}
	}
	return out
}

func (c *catalog) total(includeAgents bool) int {
	n := 0
	for _, e := range c.entries {
		if e.shown(includeAgents) {
			n++
		}
	}
	return n
}

func (c *catalog) agents() int {
	n := 0
	for _, e := range c.entries {
		if e.entrypoint == sdkEntrypoint {
			n++
		}
	}
	return n
}

// unloadedByProject counts unread shown sessions per project, for "+N more".
func (c *catalog) unloadedByProject(includeAgents bool) map[string]int {
	out := map[string]int{}
	for _, e := range c.entries {
		if e.loaded || !e.shown(includeAgents) {
			continue
		}
		if p, ok := c.dirProject[e.file.Dir]; ok {
			out[p]++
		}
	}
	return out
}

// remove forgets a session whose file moved away (trash, delete, restore).
func (c *catalog) remove(path string) {
	i, ok := c.index[path]
	if !ok {
		return
	}
	c.entries = append(c.entries[:i], c.entries[i+1:]...)
	c.reindex()
	c.dropLoaded(path)
}

// refresh re-reads a session whose file changed on disk (merge rewrites the
// target and trashes the others). A file that is gone is removed.
func (c *catalog) refresh(path string) {
	i, ok := c.index[path]
	if !ok {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		c.remove(path)
		return
	}
	e := &c.entries[i]
	e.file.Size, e.file.FileMtime, e.file.SidecarMtime = info.Size(), info.ModTime(), latestSidecarMtime(path)
	wasLoaded := e.loaded
	if wasLoaded {
		e.loaded = false
		c.dropLoaded(path)
	}
	sort.SliceStable(c.entries, func(a, b int) bool {
		return c.entries[a].file.ModTime().After(c.entries[b].file.ModTime())
	})
	c.reindex()
	if wasLoaded {
		c.load([]int{c.index[path]})
	}
}

func (c *catalog) dropLoaded(path string) {
	for k := range c.sessions {
		if c.sessions[k].Path == path {
			c.sessions = append(c.sessions[:k], c.sessions[k+1:]...)
			return
		}
	}
}

// ---------- helpers ----------

// countSubAgents returns the number of agent-*.jsonl files in the session's
// "<uuid>/subagents/" sidecar dir. Returns 0 when the dir doesn't exist.
func countSubAgents(jsonlPath string) int {
	uuid := strings.TrimSuffix(filepath.Base(jsonlPath), ".jsonl")
	dir := filepath.Join(filepath.Dir(jsonlPath), uuid, "subagents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			n++
		}
	}
	return n
}

// latestSidecarMtime walks the "<uuid>/" sibling directory of a session jsonl
// (sub-agents, tool-results, …) and returns the latest file mtime found.
// Returns the zero time when the directory is absent or empty.
func latestSidecarMtime(jsonlPath string) time.Time {
	uuid := strings.TrimSuffix(filepath.Base(jsonlPath), ".jsonl")
	dir := filepath.Join(filepath.Dir(jsonlPath), uuid)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return time.Time{}
	}
	var latest time.Time
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if t := info.ModTime(); t.After(latest) {
			latest = t
		}
		return nil
	})
	return latest
}

func extractFirstText(raw json.RawMessage) string {
	var m rawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	// Content may be a plain string or an array of content blocks
	var asString string
	if err := json.Unmarshal(m.Content, &asString); err == nil {
		return cleanText(asString)
	}
	var blocks []contentBlock
	if err := json.Unmarshal(m.Content, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return cleanText(b.Text)
			}
		}
	}
	return ""
}

func cleanText(s string) string {
	s = strings.TrimSpace(s)
	// drop system-reminder blocks (frequently leading user messages)
	if strings.HasPrefix(s, "<") {
		// strip leading XML-ish tags
		for strings.HasPrefix(s, "<") {
			end := strings.Index(s, ">")
			if end < 0 {
				break
			}
			closeTag := strings.Index(s, "</")
			if closeTag < 0 {
				break
			}
			closeEnd := strings.Index(s[closeTag:], ">")
			if closeEnd < 0 {
				break
			}
			s = strings.TrimSpace(s[closeTag+closeEnd+1:])
		}
	}
	// take first non-empty line
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func deriveProject(cwd string) string {
	if cwd == "" {
		return "(unknown)"
	}
	return filepath.Base(cwd)
}
