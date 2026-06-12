package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/sahilm/fuzzy"
)

// ---------- styles ----------

var (
	styleGroup          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	styleGroupRule      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleGroupCount     = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	styleSessionDivider = lipgloss.NewStyle().Foreground(lipgloss.Color("236"))
	styleCursorBar      = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	styleSelectedBg     = lipgloss.NewStyle().Background(lipgloss.Color("238")).Foreground(lipgloss.Color("15"))
	styleSelectedTitle  = lipgloss.NewStyle().Background(lipgloss.Color("238")).Foreground(lipgloss.Color("15")).Bold(true)
	styleDim            = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleBranch         = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleWarn           = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	// styleDup tints the "+N similar" badge — softer than warn, distinct from
	// the regular dim metadata so the user spots collapsed siblings at a glance.
	styleDup = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	// styleSubagentBadge marks sessions with sub-agent spawns so the user
	// knows `s` will reveal something. Cyan to echo other interactive cues
	// (search label, key chips).
	styleSubagentBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	styleHelp           = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleSearchLabel    = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	styleScrollHint     = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	// help line styling — key chip vs label, brighter than the old dim grey.
	styleHelpKey  = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	styleHelpDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	styleHelpSep  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	// Bright red banner used while in trash view so the user always sees
	// they're in a destructive scope.
	styleTrashBanner = lipgloss.NewStyle().
				Background(lipgloss.Color("9")).
				Foreground(lipgloss.Color("15")).
				Bold(true)
)

// pendingAction is a queued destructive operation awaiting y/n confirmation.
// We capture the session ID at the moment the prompt fires so that subsequent
// row reshuffling doesn't change which session gets acted on.
type pendingAction struct {
	sessionID string
	kind      string // "trash" — move to trash; "permdel" — permanent delete
}

// helpKey is a localised key/label pair used to render the header's key hints
// with visual hierarchy (key bright, label dim, separator dimmer).
type helpKey struct {
	key string
	en  string
	ko  string
}

func (k helpKey) label() string {
	if currentLang == LangKO {
		return k.ko
	}
	return k.en
}

// Header hint — kept to ~5 essential keys. Full key reference lives behind
// the '?' overlay, so adding new keys doesn't cause truncation here.
var helpKeysPrimary = []helpKey{
	{"↑/↓", "select", "선택"},
	{"enter", "open", "열기"},
	{"/", "filter", "필터"},
	{"?", "help", "도움말"},
	{"q", "quit", "종료"},
}

// helpEntry / helpGroup describe the full key reference shown in the '?'
// overlay. Group titles + each row's description are localised via T() lookups.
type helpEntry struct {
	keys string
	desc string // i18n key (e.g., "help.move_cursor")
}

type helpGroup struct {
	title   string // i18n key (e.g., "help.section.navigate")
	entries []helpEntry
}

var helpGroups = []helpGroup{
	{title: "help.section.navigate", entries: []helpEntry{
		{"↑/↓  j/k", "help.move_cursor"},
		{"→/←  l/h", "help.drill"},
		{"^d/^u", "help.half_page"},
		{"g/G  Home/End", "help.top_bottom"},
	}},
	{title: "help.section.session", entries: []helpEntry{
		{"enter", "help.open"},
		{"e", "help.export"},
		{"p", "help.pin"},
		{"s", "help.subagent"},
		{"space", "help.mark"},
		{"m", "help.merge"},
	}},
	{title: "help.section.manage", entries: []helpEntry{
		{"d", "help.delete"},
		{"t", "help.trash_toggle"},
		{"r", "help.restore"},
	}},
	{title: "help.section.filter", entries: []helpEntry{
		{"/", "help.filter_start"},
		{"a", "help.toggle_agents"},
		{"esc", "help.unwind"},
	}},
	{title: "help.section.other", entries: []helpEntry{
		{"?", "help.help"},
		{"q", "help.quit"},
	}},
}

// renderHelpLine renders key/label chips separated by middle dots. If
// maxWidth > 0, drops trailing items that would push the line past that width.
// Critical first — the keys at the start of the slice survive narrow terminals.
func renderHelpLine(keys []helpKey, maxWidth int) string {
	var b strings.Builder
	used := 0
	for i, k := range keys {
		chunk := styleHelpKey.Render(k.key) + " " + styleHelpDesc.Render(k.label())
		var sep string
		if i > 0 {
			sep = " " + styleHelpSep.Render("·") + " "
		}
		add := lipgloss.Width(sep) + lipgloss.Width(chunk)
		if maxWidth > 0 && used+add > maxWidth {
			break
		}
		b.WriteString(sep)
		b.WriteString(chunk)
		used += add
	}
	return b.String()
}

// ---------- model ----------

// row represents one rendered entry — either a group header, a session, or a
// "more" toggle that expands/collapses the project's remaining sessions.
type row struct {
	isGroup bool
	isMore  bool
	group   string   // for group/more rows: the project name
	session *Session // for session rows
	warn    string
	hiddenN int  // for more rows: count of hidden sessions (0 means "collapse" toggle)
	pinned  bool // marker — render with ★ in session rows
	// dupN > 0 marks a session that represents N additional sessions in the
	// same project sharing its FirstMessage. Rendered as " +N similar" so the
	// user knows the row stands in for a group of templated runs (e.g.
	// repeated `spec-to-plan` workflow invocations). Drill into the project
	// or open the row to see them.
	dupN int
	// subagent is non-nil for rows in the sub-agent drill-down view.
	subagent *SubAgent
}

// pinnedGroupName is the synthetic group name used for the pinned-sessions
// section at the top of the overview. Should never collide with a real
// project basename because of the marker chars.
const pinnedGroupName = "★ Pinned"

const (
	// header height is computed dynamically by headerHeight() because the logo
	// is hidden while filtering. footer is a single optional scroll-indicator
	// line — empty when nothing's off-screen.
	footerLines = 1
	// defaultSessionsPerGroup limits how many session rows show up per project
	// when the group is collapsed. Anything beyond that is hidden behind a
	// toggle row. 5 hits the sweet spot: large enough that small/medium projects
	// show everything, small enough that 20+ sessions don't drown the picker.
	defaultSessionsPerGroup = 5
)

type Model struct {
	all       []Session
	rows      []row
	cursor    int
	search    textinput.Model
	filtering bool

	// drillProject scopes the view to a single project's full session list.
	// "" means the default multi-project overview.
	drillProject string

	currentCWD string

	width, height int

	vp        viewport.Model
	rowLines  []int // line index in rendered content where each row begins
	totalLine int   // total line count of rendered content

	// transient status — shown in the footer until cleared or replaced.
	status        string
	statusActions string // optional "[o] open · ..." hint
	statusPath    string // path associated with a successful export, for o/c keys

	// trashView toggles between live sessions and the trash directory.
	trashView bool
	// trashAll is the loaded set of sessions in the csm trash. Refreshed when
	// entering trash view and after any delete/restore operation.
	trashAll []Session
	// pendingConfirm describes a pending y/n confirmation (currently used for
	// trash + permanent-delete). nil means no active confirmation.
	pendingConfirm *pendingAction

	// helpView replaces the picker with a full-screen key reference. Any key
	// dismisses it.
	helpView bool

	// showAgents toggles whether SDK / orchestration sessions
	// (entrypoint=="sdk-cli") are shown. Hidden by default — user-started
	// sessions are usually all the user cares about.
	showAgents bool

	// subagentView is set to the parent session whose sub-agent spawns are
	// currently being browsed. nil means the normal session picker.
	subagentView *Session
	subagents    []SubAgent

	// pins is the in-memory sidecar of starred session IDs. Saved on every
	// toggle.
	pins pinStore

	// marked is the ordered list of session IDs selected for merge (space
	// toggles). In-memory only — no sidecar; cleared on esc or after a merge.
	marked []string

	Selected *Session
	Quit     bool
}

func NewModel(sessions []Session, currentCWD string) Model {
	ti := textinput.New()
	ti.Placeholder = T("filter.placeholder")
	ti.Prompt = ""
	ti.CharLimit = 200

	vp := viewport.New(0, 0)

	pins, _ := LoadPins()
	m := Model{
		all:        sessions,
		search:     ti,
		currentCWD: currentCWD,
		vp:         vp,
		pins:       pins,
	}
	m.rebuildRows("")
	m.cursorToFirstSession()
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

// rebuildRows applies the current search filter and groups by project. When
// trashView is on, the rows are sourced from the trash sessions instead.
// When subagentView is set, the rows show that session's sub-agent spawns
// instead — a flat, time-descending list (no project grouping).
func (m *Model) rebuildRows(query string) {
	if m.subagentView != nil {
		m.rows = m.rows[:0]
		m.rows = append(m.rows, row{isGroup: true, group: subagentGroupTitle(*m.subagentView)})
		for i := range m.subagents {
			a := m.subagents[i]
			m.rows = append(m.rows, row{subagent: &a})
		}
		return
	}
	source := m.all
	if m.trashView {
		source = m.trashAll
	}
	// Hide SDK-spawned sessions unless the user toggled them on.
	// Trash view always shows everything that the user explicitly deleted.
	if !m.showAgents && !m.trashView {
		visible := source[:0:0]
		for _, s := range source {
			if isAgentSession(s) {
				continue
			}
			visible = append(visible, s)
		}
		source = visible
	}
	filtered := source
	if q := strings.TrimSpace(query); q != "" {
		hay := make([]string, len(source))
		for i, s := range source {
			hay[i] = s.Project + " " + s.FirstMessage
		}
		matches := fuzzy.Find(q, hay)
		filtered = make([]Session, 0, len(matches))
		for _, mt := range matches {
			filtered = append(filtered, source[mt.Index])
		}
	}

	byProject := map[string][]Session{}
	for _, s := range filtered {
		byProject[s.Project] = append(byProject[s.Project], s)
	}

	type grp struct {
		name     string
		sessions []Session
		latest   time.Time
	}
	groups := make([]grp, 0, len(byProject))
	currentProjectName := deriveProject(m.currentCWD)
	for name, ss := range byProject {
		var latest time.Time
		for _, s := range ss {
			if s.LastActivity.After(latest) {
				latest = s.LastActivity
			}
		}
		groups = append(groups, grp{name, ss, latest})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].name == currentProjectName {
			return true
		}
		if groups[j].name == currentProjectName {
			return false
		}
		return groups[i].latest.After(groups[j].latest)
	})

	m.rows = m.rows[:0]
	pinSet := m.pins.idSet()

	// Pinned section — only in main overview (no drill, no filter, not in
	// trash view). Pinned sessions are listed here AND remain inline in their
	// original project groups with a ★ marker.
	if m.drillProject == "" && !m.trashView && query == "" && len(pinSet) > 0 {
		pinnedList := make([]Session, 0, len(pinSet))
		for _, s := range source {
			if _, ok := pinSet[s.ID]; ok {
				pinnedList = append(pinnedList, s)
			}
		}
		if len(pinnedList) > 0 {
			m.rows = append(m.rows, row{isGroup: true, group: pinnedGroupName})
			sort.SliceStable(pinnedList, func(i, j int) bool {
				return pinnedList[i].LastActivity.After(pinnedList[j].LastActivity)
			})
			for i := range pinnedList {
				s := pinnedList[i]
				m.rows = append(m.rows, row{
					session: &s,
					warn:    computeWarn(s),
					pinned:  true,
				})
			}
		}
	}

	// Drill-down: scope the view to one project's full list. Filter mode wins
	// over drill (typing a query is a global search intent).
	if m.drillProject != "" && query == "" {
		for _, g := range groups {
			if g.name != m.drillProject {
				continue
			}
			m.rows = append(m.rows, row{isGroup: true, group: g.name})
			for i := range g.sessions {
				s := g.sessions[i]
				m.rows = append(m.rows, row{session: &s, warn: computeWarn(s)})
			}
			return
		}
		// No matches under this project — keep drill state, render an empty
		// group header so the user sees they're still drilled.
		m.rows = append(m.rows, row{isGroup: true, group: m.drillProject})
		return
	}

	// Default (overview) mode. Each group shows at most
	// defaultSessionsPerGroup sessions; the rest are hidden behind a "more"
	// row that opens the drill-down view.
	// When filtering is active we drop the cap so the user sees every match.
	cap := defaultSessionsPerGroup
	if query != "" {
		cap = -1 // unlimited
	}

	for _, g := range groups {
		// In the overview, collapse sessions that share an identical FirstMessage
		// (templated agentic workflows tend to spawn many such siblings). The
		// kept session is the most recent one, with a "+N similar" badge. The
		// rest stay reachable by drilling into the project.
		repsByMsg := map[string]int{} // FirstMessage -> index into reps
		reps := make([]Session, 0, len(g.sessions))
		dupCounts := make([]int, 0, len(g.sessions))
		for _, s := range g.sessions {
			msg := strings.TrimSpace(s.FirstMessage)
			if msg == "" {
				// Empty messages shouldn't collide with each other — emit each
				// as its own row so unrelated sessions don't lump together.
				reps = append(reps, s)
				dupCounts = append(dupCounts, 0)
				continue
			}
			if idx, ok := repsByMsg[msg]; ok {
				dupCounts[idx]++
				continue
			}
			repsByMsg[msg] = len(reps)
			reps = append(reps, s)
			dupCounts = append(dupCounts, 0)
		}

		m.rows = append(m.rows, row{isGroup: true, group: g.name})
		visible := len(reps)
		if cap > 0 && visible > cap {
			visible = cap
		}
		for i := 0; i < visible; i++ {
			s := reps[i]
			_, pinned := pinSet[s.ID]
			m.rows = append(m.rows, row{
				session: &s,
				warn:    computeWarn(s),
				pinned:  pinned,
				dupN:    dupCounts[i],
			})
		}
		if hidden := len(reps) - visible; hidden > 0 {
			m.rows = append(m.rows, row{
				isMore:  true,
				group:   g.name,
				hiddenN: hidden,
			})
		}
	}
}

func computeWarn(s Session) string {
	if s.CWD == "" {
		return "no cwd"
	}
	return ""
}

// subagentGroupTitle returns the group header text shown above the sub-agent
// list. Includes the parent session's first-message preview so the user
// remembers what they drilled into.
func subagentGroupTitle(s Session) string {
	title := oneLine(s.FirstMessage)
	if title == "" {
		title = T("no_message")
	}
	const maxRunes = 60
	r := []rune(title)
	if len(r) > maxRunes {
		title = string(r[:maxRunes]) + "…"
	}
	return T("subagent.group_prefix") + "  " + title
}

func (m *Model) cursorToFirstSession() {
	for i, r := range m.rows {
		if !r.isGroup {
			m.cursor = i
			return
		}
	}
	m.cursor = 0
}

func (m *Model) cursorToLastSession() {
	for i := len(m.rows) - 1; i >= 0; i-- {
		if !m.rows[i].isGroup {
			m.cursor = i
			return
		}
	}
}

func (m *Model) moveCursor(d int) {
	if len(m.rows) == 0 {
		return
	}
	i := m.cursor + d
	for i >= 0 && i < len(m.rows) {
		if !m.rows[i].isGroup {
			m.cursor = i
			return
		}
		i += d
	}
}

// moveCursorBySessions advances the cursor by n session entries (skipping group headers).
// Negative n moves backward. Clamps at the first/last session.
func (m *Model) moveCursorBySessions(n int) {
	if len(m.rows) == 0 || n == 0 {
		return
	}
	step := 1
	if n < 0 {
		step = -1
		n = -n
	}
	i := m.cursor
	for n > 0 {
		next := i + step
		if next < 0 || next >= len(m.rows) {
			break
		}
		i = next
		if !m.rows[i].isGroup {
			n--
			m.cursor = i
		}
	}
}

func (m Model) currentSession() *Session {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor].session
}

// drillIn enters drill-down for the cursor's project (overview → drill).
// No-op when already drilled or cursor isn't on a project-bearing row.
func (m *Model) drillIn() {
	if m.drillProject != "" || m.cursor < 0 || m.cursor >= len(m.rows) {
		return
	}
	r := m.rows[m.cursor]
	var project, keepID string
	switch {
	case r.isMore:
		project = r.group
	case r.session != nil:
		project = r.session.Project
		keepID = r.session.ID
	default:
		return
	}
	if project == "" {
		return
	}
	m.drillProject = project
	m.rebuildRows("")
	if !m.landCursorOnSession(keepID) {
		m.cursorToFirstSession()
	}
	m.rebuildContent()
	m.scrollToCursor()
}

// drillOut returns to the overview from drill-down. The cursor lands on the
// same session if it's visible in the overview; otherwise on the project's
// "more" toggle so the user can drill back in with one keypress.
func (m *Model) drillOut() {
	if m.drillProject == "" {
		return
	}
	var keepID string
	if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].session != nil {
		keepID = m.rows[m.cursor].session.ID
	}
	exited := m.drillProject
	m.drillProject = ""
	m.rebuildRows("")
	if !m.landCursorOnSession(keepID) && !m.landCursorOnMoreOf(exited) {
		m.cursorToFirstSession()
	}
	m.rebuildContent()
	m.scrollToCursor()
}

// landCursorOnSession positions the cursor on the row matching id. Returns
// true on success.
func (m *Model) landCursorOnSession(id string) bool {
	if id == "" {
		return false
	}
	for i, r := range m.rows {
		if r.session != nil && r.session.ID == id {
			m.cursor = i
			return true
		}
	}
	return false
}

// landCursorOnMoreOf positions the cursor on the project's "더보기" toggle
// row. Returns true on success.
func (m *Model) landCursorOnMoreOf(project string) bool {
	for i, r := range m.rows {
		if r.isMore && r.group == project {
			m.cursor = i
			return true
		}
	}
	return false
}

// exportCursor writes the cursor's session to a markdown file under the user's
// default csm-exports dir and sets a status message with [o]/[c] actions.
func (m *Model) exportCursor() {
	s := m.currentSession()
	if s == nil {
		return
	}
	path, err := ExportSessionToFile(*s, "")
	if err != nil {
		m.status = fmt.Sprintf(T("export.failed"), err)
		m.statusActions = ""
		m.statusPath = ""
		return
	}
	m.status = fmt.Sprintf(T("export.success"), path)
	m.statusActions = T("export.actions")
	m.statusPath = path
}

// openStatusPath asks the OS to open the file referenced by the last successful
// export — usually a markdown viewer (Obsidian, Typora, VS Code, TextEdit).
func (m *Model) openStatusPath() {
	if m.statusPath == "" {
		return
	}
	// fire-and-forget; we don't want to block the TUI on the viewer's startup
	_ = openInOS(m.statusPath)
}

// copyStatusPath places the export path on the system clipboard.
func (m *Model) copyStatusPath() {
	if m.statusPath == "" {
		return
	}
	if err := copyToClipboard(m.statusPath); err == nil {
		m.status = T("export.copied")
		m.statusActions = ""
	}
}

// markOrderOf returns the 1-based selection order of a session id among the
// merge marks, or 0 when it isn't marked.
func (m *Model) markOrderOf(id string) int {
	for i, mid := range m.marked {
		if mid == id {
			return i + 1
		}
	}
	return 0
}

// toggleMark adds/removes the cursor's session from the merge selection.
// Marking is only meaningful in the live picker — disabled in trash and
// sub-agent views, where the rows aren't resumable user sessions.
func (m *Model) toggleMark() {
	if m.trashView || m.subagentView != nil {
		return
	}
	s := m.currentSession()
	if s == nil {
		m.status = T("merge.no_target")
		m.statusActions = ""
		return
	}
	for i, id := range m.marked {
		if id == s.ID {
			m.marked = append(m.marked[:i], m.marked[i+1:]...)
			return
		}
	}
	m.marked = append(m.marked, s.ID)
}

// mergeDoneMsg is delivered when the async claude consolidation finishes.
type mergeDoneMsg struct {
	targetID string
	foldedN  int
	err      error
}

// startMerge resolves the marked sessions and returns a tea.Cmd that runs the
// (claude-backed, blocking) consolidation off the UI goroutine. The bool is
// false when there's nothing to do (fewer than 2 valid marks remain).
func (m *Model) startMerge() (tea.Cmd, bool) {
	var sel []Session
	for _, id := range m.marked {
		for i := range m.all {
			if m.all[i].ID == id {
				sel = append(sel, m.all[i])
				break
			}
		}
	}
	if len(sel) < 2 {
		m.status = T("merge.need_two")
		m.statusActions = ""
		m.statusPath = ""
		return nil, false
	}
	m.status = T("merge.running")
	m.statusActions = ""
	m.statusPath = ""
	return func() tea.Msg {
		targetID, foldedN, err := MergeConsolidate(sel)
		return mergeDoneMsg{targetID: targetID, foldedN: foldedN, err: err}
	}, true
}

// cursorToSessionID parks the cursor on the row for the given session id,
// falling back to the first session when it isn't found (e.g. hidden behind a
// collapsed project's "more" toggle).
func (m *Model) cursorToSessionID(id string) {
	for i, r := range m.rows {
		if r.session != nil && r.session.ID == id {
			m.cursor = i
			return
		}
	}
	m.cursorToFirstSession()
}

// handleDelete arms a y/n confirmation for the cursor's session. The action
// itself (trash move or permanent delete) runs in executePendingConfirm after
// the user confirms with y/Y/Enter.
func (m *Model) handleDelete() {
	s := m.currentSession()
	if s == nil {
		// On a non-session row (group header, "더보기" toggle). Give visible
		// feedback so the user knows the keypress was seen but had no target.
		m.status = T("trash.no_target")
		m.statusActions = ""
		m.statusPath = ""
		return
	}
	kind := "trash"
	prompt := T("trash.confirm_prompt")
	if m.trashView {
		kind = "permdel"
		prompt = T("trash.permdel_prompt")
	}
	m.pendingConfirm = &pendingAction{sessionID: s.ID, kind: kind}
	m.status = prompt
	m.statusActions = ""
	m.statusPath = ""
}

// executePendingConfirm performs the action the user just confirmed (y).
// Cleared whether or not the action succeeded.
func (m *Model) executePendingConfirm() {
	if m.pendingConfirm == nil {
		return
	}
	target := m.findSessionByID(m.pendingConfirm.sessionID)
	kind := m.pendingConfirm.kind
	m.pendingConfirm = nil
	if target == nil {
		m.status = T("trash.no_target")
		return
	}
	switch kind {
	case "permdel":
		if err := PermanentlyDelete(target.Path); err != nil {
			m.status = fmt.Sprintf(T("trash.error"), err)
			return
		}
		m.status = T("trash.permdel_done")
		ts, _ := LoadTrashSessions()
		m.trashAll = ts
	default: // "trash"
		if _, err := MoveToTrash(*target); err != nil {
			m.status = fmt.Sprintf(T("trash.error"), err)
			return
		}
		m.status = T("trash.moved")
		live, _ := LoadSessions()
		m.all = live
	}
	m.statusActions = ""
	m.refreshAfterMutation()
}

// cancelPendingConfirm dismisses an outstanding confirmation prompt.
func (m *Model) cancelPendingConfirm() {
	m.pendingConfirm = nil
	m.status = ""
	m.statusActions = ""
}

// findSessionByID returns a pointer to a Session in the current scope (live or
// trash) matching id, or nil. Uses the loaded slice so it reflects the latest
// state.
func (m *Model) findSessionByID(id string) *Session {
	source := m.all
	if m.trashView {
		source = m.trashAll
	}
	for i := range source {
		if source[i].ID == id {
			return &source[i]
		}
	}
	return nil
}

// toggleTrashView switches between live and trash views. Loads trash sessions
// on the way in.
func (m *Model) toggleTrashView() {
	if m.trashView {
		m.trashView = false
	} else {
		ts, err := LoadTrashSessions()
		if err != nil {
			m.status = fmt.Sprintf(T("trash.error"), err)
			return
		}
		m.trashAll = ts
		m.trashView = true
	}
	m.drillProject = ""
	m.pendingConfirm = nil
	m.status = ""
	m.statusActions = ""
	m.rebuildRows("")
	m.cursorToFirstSession()
	m.rebuildContent()
	m.scrollToCursor()
}

// openSubagentFile opens the cursor's sub-agent jsonl in the OS default
// viewer. Path is also placed in m.statusPath so `o` / `c` continue to work.
func (m *Model) openSubagentFile() {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return
	}
	a := m.rows[m.cursor].subagent
	if a == nil {
		return
	}
	if err := openInOS(a.Path); err != nil {
		m.status = fmt.Sprintf(T("export.failed"), err)
		return
	}
	m.status = T("export.opening")
	m.statusActions = "[o] " + T("status.open") + " · [c] " + T("status.copy")
	m.statusPath = a.Path
}

// enterSubagentView drills into the cursor session's sub-agent spawns.
// No-op when the cursor isn't on a session or the session has no subagents.
func (m *Model) enterSubagentView() {
	if m.subagentView != nil {
		return
	}
	if m.filtering {
		return
	}
	s := m.currentSession()
	if s == nil {
		return
	}
	agents, err := LoadSubAgents(*s)
	if err != nil || len(agents) == 0 {
		m.status = T("subagent.none")
		m.statusActions = ""
		return
	}
	sess := *s // copy so the pointer stays stable across rebuilds
	m.subagentView = &sess
	m.subagents = agents
	m.pendingConfirm = nil
	m.status = ""
	m.statusActions = ""
	m.rebuildRows("")
	m.cursorToFirstSession()
	m.rebuildContent()
	m.scrollToCursor()
}

// leaveSubagentView returns from the sub-agent drill-down to the prior view.
func (m *Model) leaveSubagentView() {
	m.subagentView = nil
	m.subagents = nil
	m.status = ""
	m.statusActions = ""
	m.rebuildRows(m.search.Value())
	m.cursorToFirstSession()
	m.rebuildContent()
	m.scrollToCursor()
}

// toggleAgents flips visibility of SDK-spawned sessions (worktree
// orchestration, sub-process Claude runs, etc.). Hidden by default.
func (m *Model) toggleAgents() {
	if m.trashView {
		// Trash view already shows everything; toggle is a no-op there.
		return
	}
	m.showAgents = !m.showAgents
	if m.showAgents {
		m.status = T("agents.shown")
	} else {
		m.status = T("agents.hidden")
	}
	m.statusActions = ""
	m.pendingConfirm = nil
	q := m.search.Value()
	m.rebuildRows(q)
	m.cursorToFirstSession()
	m.rebuildContent()
	m.scrollToCursor()
}

// isAgentSession returns true if the session was launched by the SDK
// (orchestration tools, worktree agents) rather than directly by the user.
func isAgentSession(s Session) bool {
	return s.Entrypoint == "sdk-cli"
}

// countHiddenAgents returns the number of agent sessions filtered out of
// the live view. Used by the header to surface "+N agent sessions hidden".
func (m Model) countHiddenAgents() int {
	if m.showAgents || m.trashView {
		return 0
	}
	n := 0
	for _, s := range m.all {
		if isAgentSession(s) {
			n++
		}
	}
	return n
}

// restoreCursor moves the cursor's trashed session back to live storage.
func (m *Model) restoreCursor() {
	s := m.currentSession()
	if s == nil {
		return
	}
	if err := RestoreFromTrash(s.Path); err != nil {
		m.status = fmt.Sprintf(T("trash.error"), err)
		return
	}
	m.status = T("trash.restore_done")
	ts, _ := LoadTrashSessions()
	m.trashAll = ts
	live, _ := LoadSessions()
	m.all = live
	m.refreshAfterMutation()
}

// togglePin flips the pin state of the cursor's session and persists the
// sidecar. Pinned sessions show in the ★ Pinned section at the top of the
// overview AND remain inline in their project group with a ★ marker.
func (m *Model) togglePin() {
	s := m.currentSession()
	if s == nil {
		return
	}
	added := m.pins.Toggle(s.ID, oneLine(s.FirstMessage))
	if err := SavePins(m.pins); err != nil {
		m.status = fmt.Sprintf(T("pin.error"), err)
		// roll back in-memory state to match disk
		m.pins.Toggle(s.ID, "")
		return
	}
	if added {
		m.status = T("pin.added")
	} else {
		m.status = T("pin.removed")
	}
	m.statusActions = ""
	m.refreshAfterMutation()
}

// refreshAfterMutation rebuilds rows + content + cursor + viewport after a
// trash/restore/delete that changed the underlying data.
func (m *Model) refreshAfterMutation() {
	m.rebuildRows("")
	if m.cursor >= len(m.rows) {
		m.cursorToFirstSession()
	} else if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].isGroup {
		m.moveCursor(1)
	}
	m.rebuildContent()
	m.scrollToCursor()
}

// totalSessions returns the count of unique sessions in the current view.
// Skips group headers, "더보기" toggle rows, and de-duplicates sessions that
// appear in both the ★ Pinned section and their original project group.
func (m Model) totalSessions() int {
	seen := map[string]struct{}{}
	for _, r := range m.rows {
		if r.session == nil {
			continue
		}
		seen[r.session.ID] = struct{}{}
	}
	return len(seen)
}

// cursorSessionIndex returns the 1-based position of the cursor among unique
// sessions. Sessions that appear twice (★ Pinned + their group) only count
// once; "더보기" / group-header rows don't count at all.
func (m Model) cursorSessionIndex() int {
	seen := map[string]struct{}{}
	for i, r := range m.rows {
		if r.session != nil {
			if _, dup := seen[r.session.ID]; !dup {
				seen[r.session.ID] = struct{}{}
			}
		}
		if i == m.cursor {
			return len(seen)
		}
	}
	return 0
}

// ---------- rendering ----------

// rebuildContent renders the full row list to a string, records line positions for
// each row, and pushes the result into the viewport.
func (m *Model) rebuildContent() {
	width := m.width
	if width <= 0 {
		width = 80
	}

	var b strings.Builder
	m.rowLines = make([]int, len(m.rows))
	line := 0
	firstGroup := true
	prevWasSession := false

	for i, r := range m.rows {
		if r.isGroup {
			if !firstGroup {
				b.WriteString("\n")
				line++
			}
			firstGroup = false
			prevWasSession = false
			m.rowLines[i] = line

			count := 0
			for j := i + 1; j < len(m.rows); j++ {
				if m.rows[j].isGroup {
					break
				}
				count++
			}

			header := r.group
			countStr := fmt.Sprintf(" %d", count)
			used := lipgloss.Width(header) + lipgloss.Width(countStr) + 2
			ruleLen := width - used
			if ruleLen < 4 {
				ruleLen = 4
			}
			rule := strings.Repeat("─", ruleLen)
			b.WriteString(styleGroup.Render(header))
			b.WriteString(styleGroupCount.Render(countStr))
			b.WriteString(" ")
			b.WriteString(styleGroupRule.Render(rule))
			b.WriteString("\n")
			line++
			continue
		}

		if r.isMore {
			// the "더보기" toggle — single-line row that drops the user into the
			// drill-down view. Visually distinct from session rows: no divider
			// above, dim text with a chevron, same selectable highlight.
			m.rowLines[i] = line
			renderMoreRow(&b, r.hiddenN, i == m.cursor, width)
			line++
			prevWasSession = false
			continue
		}

		if prevWasSession {
			divLen := width - 4
			if divLen < 10 {
				divLen = 10
			}
			b.WriteString("  " + styleSessionDivider.Render(strings.Repeat("┄", divLen)) + "\n")
			line++
		}
		prevWasSession = true

		m.rowLines[i] = line
		var n int
		if r.subagent != nil {
			n = renderSubAgentRow(&b, r.subagent, i == m.cursor, width)
		} else {
			n = renderSessionRow(&b, r.session, r.warn, i == m.cursor, r.pinned, r.dupN, m.markOrderOf(r.session.ID), width)
		}
		line += n
	}

	m.totalLine = line
	m.vp.SetContent(b.String())
}

// scrollToCursor adjusts the viewport so the cursor's session row is visible.
// Each session occupies 2 lines starting at rowLines[cursor]. As a courtesy, we
// also try to keep the cursor's group header visible above it — when the group
// header would fit within the viewport without pushing the cursor off-screen,
// we scroll so the header is the top line. This avoids the "I scrolled back
// up but the project name is gone" UX problem.
func (m *Model) scrollToCursor() {
	if m.cursor < 0 || m.cursor >= len(m.rowLines) {
		return
	}
	// row height: session rows are 2–3 lines (3 when the badges wrap),
	// more-row is 1. Derive from rowLines deltas so we don't have to
	// re-evaluate the wrap condition.
	cursorTop := m.rowLines[m.cursor]
	var cursorBottom int
	if m.cursor+1 < len(m.rowLines) {
		// Subtract 1 because the inter-row divider/blank line lives between
		// rowLines[cursor] and rowLines[cursor+1] but counts toward the
		// *next* row's preamble, not the current row's content.
		next := m.rowLines[m.cursor+1]
		// Divider/blank-line adjustment: rebuildContent inserts 1 blank line
		// before a new group and 1 divider before a non-first session row in
		// the same group. Subtract whichever applies so cursorBottom reflects
		// only the cursor row's own lines.
		preamble := 0
		switch {
		case m.rows[m.cursor+1].isGroup:
			preamble = 1 // blank line above the group header
		case m.rows[m.cursor+1].session != nil || m.rows[m.cursor+1].subagent != nil:
			preamble = 1 // session/sub-agent divider
		}
		cursorBottom = next - preamble - 1
	} else {
		cursorBottom = m.totalLine - 1
	}
	if cursorBottom < cursorTop {
		cursorBottom = cursorTop
	}

	// Step 1 — make sure the cursor itself is visible.
	vpTop := m.vp.YOffset
	vpBottom := vpTop + m.vp.Height - 1
	if cursorTop < vpTop {
		m.vp.SetYOffset(cursorTop)
	} else if cursorBottom > vpBottom {
		m.vp.SetYOffset(cursorBottom - m.vp.Height + 1)
	}

	// Step 2 — try to also include the group header above this row, if it fits.
	groupRow := -1
	for i := m.cursor - 1; i >= 0; i-- {
		if m.rows[i].isGroup {
			groupRow = i
			break
		}
	}
	if groupRow < 0 {
		return
	}
	groupLine := m.rowLines[groupRow]
	if groupLine >= m.vp.YOffset {
		return // already visible
	}
	// Pull viewport up to start at the group header, but only if cursor stays in view.
	if cursorBottom <= groupLine+m.vp.Height-1 {
		m.vp.SetYOffset(groupLine)
	}
}

func (m *Model) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.vp.Width = m.width
	h := m.height - m.headerHeight() - footerLines
	if h < 1 {
		h = 1
	}
	m.vp.Height = h
}

// headerHeight returns the number of lines the header consumes for the current
// state. Filtering shows a compact single-line search; otherwise the full ASCII
// logo block.
func (m Model) headerHeight() int {
	if m.filtering {
		return 2
	}
	if !m.canShowLogo() {
		return 2
	}
	return 7 // 6 logo lines + 1 blank
}

// canShowLogo hides the logo on terminals too short to spare the lines.
func (m Model) canShowLogo() bool {
	return m.height >= 20
}

// ---------- bubbletea protocol ----------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		m.rebuildContent()
		m.scrollToCursor()
		return m, nil

	case mergeDoneMsg:
		if msg.err != nil {
			m.status = fmt.Sprintf(T("merge.failed"), msg.err)
			m.statusActions = ""
			m.statusPath = ""
			return m, nil
		}
		m.marked = nil
		if live, e := LoadSessions(); e == nil {
			m.all = live
		}
		m.rebuildRows(m.search.Value())
		m.cursorToSessionID(msg.targetID)
		m.status = fmt.Sprintf(T("merge.success"), msg.foldedN)
		m.statusActions = T("merge.actions")
		m.statusPath = ""
		m.rebuildContent()
		m.scrollToCursor()
		return m, nil

	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.moveCursor(-1)
		case tea.MouseButtonWheelDown:
			m.moveCursor(1)
		default:
			return m, nil
		}
		m.rebuildContent()
		m.scrollToCursor()
		return m, nil

	case tea.KeyMsg:
		if m.filtering {
			switch msg.Type {
			case tea.KeyEsc:
				m.filtering = false
				m.search.Blur()
				m.search.SetValue("")
				m.rebuildRows("")
				m.cursorToFirstSession()
				m.resize() // header height changed
				m.rebuildContent()
				m.scrollToCursor()
				return m, nil
			case tea.KeyEnter:
				if s := m.currentSession(); s != nil {
					m.Selected = s
					return m, tea.Quit
				}
				return m, nil
			case tea.KeyUp, tea.KeyCtrlP:
				m.moveCursor(-1)
				m.rebuildContent()
				m.scrollToCursor()
				return m, nil
			case tea.KeyDown, tea.KeyCtrlN:
				m.moveCursor(1)
				m.rebuildContent()
				m.scrollToCursor()
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			m.rebuildRows(m.search.Value())
			if m.cursor >= len(m.rows) || (m.cursor < len(m.rows) && m.rows[m.cursor].isGroup) {
				m.cursorToFirstSession()
			}
			m.rebuildContent()
			m.scrollToCursor()
			return m, cmd
		}

		key := msg.String()

		// Help overlay — any key dismisses, no further handling.
		if m.helpView {
			m.helpView = false
			return m, nil
		}

		// A y/n confirmation prompt is exclusive — it intercepts most keys.
		if m.pendingConfirm != nil {
			switch key {
			case "y", "Y", "enter":
				m.executePendingConfirm()
			case "n", "N", "esc", "ctrl+c", "q":
				m.cancelPendingConfirm()
			default:
				// any other key cancels (less surprising than ignoring it)
				m.cancelPendingConfirm()
			}
			return m, nil
		}

		switch key {
		case "q", "ctrl+c":
			m.Quit = true
			return m, tea.Quit
		case "esc":
			// ESC unwinds the current "mode" — status banner, sub-agent view,
			// drill-down, trash view — before quitting at the top level.
			if m.status != "" {
				m.status = ""
				m.statusActions = ""
				m.statusPath = ""
				return m, nil
			}
			if len(m.marked) > 0 {
				m.marked = nil
				m.rebuildContent()
				m.scrollToCursor()
				return m, nil
			}
			if m.subagentView != nil {
				m.leaveSubagentView()
				return m, nil
			}
			if m.drillProject != "" {
				m.drillOut()
				return m, nil
			}
			if m.trashView {
				m.toggleTrashView()
				return m, nil
			}
			m.Quit = true
			return m, tea.Quit
		case "enter":
			// If the cursor sits on a "더보기" row, enter the drill-down view.
			if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].isMore {
				m.drillProject = m.rows[m.cursor].group
				m.rebuildRows("")
				m.cursorToFirstSession()
				m.rebuildContent()
				m.scrollToCursor()
				return m, nil
			}
			// Sub-agent rows aren't resumable Claude sessions — open the jsonl
			// in the OS default viewer instead.
			if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].subagent != nil {
				m.openSubagentFile()
				return m, nil
			}
			if s := m.currentSession(); s != nil {
				m.Selected = s
				return m, tea.Quit
			}
		case "j", "down":
			m.moveCursor(1)
		case "k", "up":
			m.moveCursor(-1)
		case "right", "l":
			m.drillIn()
		case "left", "h":
			m.drillOut()
		case "e":
			m.exportCursor()
		case " ":
			m.toggleMark()
		case "m":
			if cmd, ok := m.startMerge(); ok {
				m.rebuildContent()
				m.scrollToCursor()
				return m, cmd
			}
			return m, nil
		case "o":
			if m.statusPath != "" {
				m.openStatusPath()
			}
		case "c":
			if m.statusPath != "" {
				m.copyStatusPath()
			}
		case "d":
			m.handleDelete()
		case "t":
			m.toggleTrashView()
		case "a":
			m.toggleAgents()
		case "s":
			m.enterSubagentView()
		case "p":
			m.togglePin()
		case "r":
			if m.trashView {
				m.restoreCursor()
			}
		case "u":
			if m.trashView {
				m.restoreCursor()
			}
		case "?":
			m.helpView = true
			return m, nil
		case "ctrl+d":
			step := m.vp.Height / 4 // each session is ~2 lines + divider; conservative
			if step < 1 {
				step = 1
			}
			m.moveCursorBySessions(step)
		case "ctrl+u":
			step := m.vp.Height / 4
			if step < 1 {
				step = 1
			}
			m.moveCursorBySessions(-step)
		case "ctrl+f", "pgdown":
			step := m.vp.Height / 2
			if step < 1 {
				step = 1
			}
			m.moveCursorBySessions(step)
		case "ctrl+b", "pgup":
			step := m.vp.Height / 2
			if step < 1 {
				step = 1
			}
			m.moveCursorBySessions(-step)
		case "g", "home":
			m.cursorToFirstSession()
		case "G", "end":
			m.cursorToLastSession()
		case "/":
			m.filtering = true
			m.search.Focus()
			m.resize() // header height changed
			m.rebuildContent()
			m.scrollToCursor()
			return m, textinput.Blink
		}
		m.rebuildContent()
		m.scrollToCursor()
	}
	return m, nil
}

// renderHelp draws the full key reference shown when m.helpView is true.
// Replaces the entire picker view. Any key dismisses (handled in Update).
func (m Model) renderHelp() string {
	var b strings.Builder
	b.WriteString("\n  ")
	b.WriteString(styleSearchLabel.Render(T("help.title")))
	b.WriteString("    ")
	b.WriteString(styleHelpDesc.Render(T("help.dismiss")))
	b.WriteString("\n\n")

	for _, g := range helpGroups {
		b.WriteString("  ")
		b.WriteString(styleTagline.Render(T(g.title)))
		b.WriteString("\n")
		for _, e := range g.entries {
			// fixed-width key column for alignment
			keyCol := e.keys
			pad := 14 - runewidth.StringWidth(keyCol)
			if pad < 1 {
				pad = 1
			}
			b.WriteString("    ")
			b.WriteString(styleHelpKey.Render(keyCol))
			b.WriteString(strings.Repeat(" ", pad))
			b.WriteString(styleHelpDesc.Render(T(e.desc)))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderHeader returns the topmost block of the TUI.
//   - while filtering: single-line search input + blank
//   - otherwise (and tall enough): 5-line ASCII logo with side metadata + blank
//   - very short terminals: compact 1-line header + blank
func (m Model) renderHeader() string {
	if m.filtering {
		return styleSearchLabel.Render("/ ") + m.search.View() + "\n\n"
	}

	total := m.totalSessions()
	pos := m.cursorSessionIndex()
	counter := fmt.Sprintf("%d / %d", pos, total)
	visibleAll := len(m.all)
	if hidden := m.countHiddenAgents(); hidden > 0 {
		visibleAll -= hidden
	}
	if total != visibleAll {
		counter += "  " + fmt.Sprintf(T("of_total"), visibleAll)
	}

	if !m.canShowLogo() {
		var b strings.Builder
		b.WriteString(styleAccent.Render("◆ "))
		b.WriteString(styleSearchLabel.Render(T("header.csm")))
		b.WriteString(styleVersion.Render("  v" + Version))
		b.WriteString(styleDim.Render("  " + counter))
		b.WriteString("\n\n")
		return b.String()
	}

	// Full logo + right-side metadata aligned to logo lines.
	// Logo is 6 lines; the right column carries name/version, tagline, counter,
	// and the two-line key reference (so users don't have to read the footer).
	logo := strings.Split(logoArt, "\n")
	// Right column shares horizontal space with the 6-line ASCII logo
	// (~28 cols) plus a 5-col gap. Anything past that is what the help line
	// gets to use, minus a small safety margin.
	const logoCols, gap, margin = 28, 5, 2
	helpMax := m.width - logoCols - gap - margin
	if helpMax < 20 {
		helpMax = 20
	}

	// In trash view, replace the tagline line with a bright banner so the
	// user always sees they're in a destructive context.
	titleLine := styleSearchLabel.Render(T("header.csm")) + "  " + styleVersion.Render("v"+Version)
	taglineLine := styleTagline.Render("Claude Code session manager")
	if m.trashView {
		titleLine = styleTrashBanner.Render(" "+T("trash.banner")+" ") + "  " + styleVersion.Render("v"+Version)
		taglineLine = styleDim.Render("press esc or t to return to live sessions")
	}

	// Secondary slot shows context-sensitive hints — e.g., the destructive-
	// keys reminder while in trash view, or the "+N agent sessions hidden"
	// reminder in the live view. Empty otherwise (full reference lives behind '?').
	contextLine := ""
	if m.trashView {
		contextLine = styleHelp.Render(T("help.trash_hint"))
	} else if hidden := m.countHiddenAgents(); hidden > 0 {
		contextLine = styleHelp.Render(fmt.Sprintf(T("agents.hidden_count"), hidden))
	}

	rightLines := []string{
		"",
		titleLine,
		taglineLine,
		styleDim.Render(counter + " sessions"),
		renderHelpLine(helpKeysPrimary, helpMax),
		contextLine,
	}

	var b strings.Builder
	for i, l := range logo {
		b.WriteString(styleLogo.Render(l))
		if i < len(rightLines) && rightLines[i] != "" {
			b.WriteString("     " + rightLines[i])
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

func (m Model) View() string {
	if m.helpView {
		return m.renderHelp()
	}
	var b strings.Builder

	// header
	b.WriteString(m.renderHeader())

	// scrollable viewport
	b.WriteString(m.vp.View())

	// footer is rendered on its own line beneath the viewport. bubbles/viewport
	// doesn't terminate its output with a newline, so we add one ourselves —
	// without this, the footer text gets appended to the last viewport line
	// and stays invisible.
	b.WriteString("\n")

	// footer — status message takes precedence; otherwise scroll indicator
	// (or filter-mode help when filtering).
	switch {
	case m.status != "":
		b.WriteString(styleTagline.Render(m.status))
		if m.statusActions != "" {
			b.WriteString("  ")
			b.WriteString(styleHelp.Render(m.statusActions))
		}
	case m.filtering:
		b.WriteString(styleHelp.Render(T("footer.filter")))
	case len(m.marked) > 0:
		b.WriteString(styleTagline.Render(fmt.Sprintf(T("merge.selected"), len(m.marked))))
	default:
		var above, below bool
		if m.vp.YOffset > 0 {
			above = true
		}
		if m.vp.YOffset+m.vp.Height < m.totalLine {
			below = true
		}
		if above || below {
			var key string
			switch {
			case above && below:
				key = "more.both"
			case above:
				key = "more.above"
			case below:
				key = "more.below"
			}
			b.WriteString(styleScrollHint.Render(T(key)))
		}
	}
	return b.String()
}

// ---------- session row rendering ----------

// renderMoreRow writes the single-line "▾ N more (enter to expand)" toggle.
// Same selectable highlight conventions as session rows.
func renderMoreRow(b *strings.Builder, hidden int, selected bool, width int) {
	contentW := width - 4
	if contentW < 10 {
		contentW = 10
	}
	text := fmt.Sprintf(T("more.show"), hidden)
	if runewidth.StringWidth(text) > contentW {
		text = runewidth.Truncate(text, contentW, "…")
	}

	if selected {
		pad := text + strings.Repeat(" ", contentW-runewidth.StringWidth(text))
		bar := styleCursorBar.Render("▌ ")
		b.WriteString("  " + bar + styleSelectedBg.Render(pad) + "\n")
		return
	}
	b.WriteString("    " + styleDim.Render(text) + "\n")
}

// renderSessionRow writes a session card (2 or 3 lines) and returns the line
// count it consumed. Selected rows get a colored left bar + filled background
// on every line. Pinned rows get a ★ prefix on the title. dupN > 0 marks a
// representative-of-N row.
//
// When the meta line (branch · ago · msgs) plus the badges (↳N agents,
// +N similar) would overflow contentW, the badges wrap onto a third line so
// the sub-agent cue stays visible regardless of how long the branch name is.
func renderSessionRow(b *strings.Builder, s *Session, warn string, selected, pinned bool, dupN, markOrder, width int) int {
	contentW := width - 4
	if contentW < 20 {
		contentW = 20
	}

	title := s.FirstMessage
	hasTitle := title != ""
	if !hasTitle {
		title = T("no_message")
	}
	if pinned {
		title = "★ " + title
	}
	if markOrder > 0 {
		title = fmt.Sprintf("[%d] ", markOrder) + title
	}
	if runewidth.StringWidth(title) > contentW {
		title = runewidth.Truncate(title, contentW, "…")
	}

	branch := s.GitBranch
	if branch == "" {
		branch = "—"
	}
	corePlain := fmt.Sprintf("%s · %s · %d %s", branch, humanizeAgo(s.LastActivity), s.MessageCount, T("msgs"))
	if warn != "" {
		corePlain += "  ⚠ " + warn
	}

	// Build the trailing badge string (sub-agent + dup). When present and
	// the combined line would overflow, the badges drop onto a third line.
	var badgesPlain string
	if s.SubAgentCount > 0 {
		badgesPlain += "  " + fmt.Sprintf(T("subagent.badge"), s.SubAgentCount)
	}
	if dupN > 0 {
		badgesPlain += "  " + fmt.Sprintf(T("dup.suffix"), dupN)
	}
	badgesPlain = strings.TrimLeft(badgesPlain, " ")

	inlinePlain := corePlain
	if badgesPlain != "" {
		inlinePlain = corePlain + "  " + badgesPlain
	}

	wrap := badgesPlain != "" && runewidth.StringWidth(inlinePlain) > contentW
	metaPlain := corePlain
	if !wrap && badgesPlain != "" {
		metaPlain = inlinePlain
	}
	metaTruncated := false
	if runewidth.StringWidth(metaPlain) > contentW {
		metaPlain = runewidth.Truncate(metaPlain, contentW, "…")
		metaTruncated = true
	}

	// Styled badge fragment for the un-truncated path. Reused on inline or
	// wrapped layout.
	badgesStyled := ""
	if s.SubAgentCount > 0 {
		badgesStyled += styleSubagentBadge.Render(fmt.Sprintf(T("subagent.badge"), s.SubAgentCount))
	}
	if dupN > 0 {
		if badgesStyled != "" {
			badgesStyled += "  "
		}
		badgesStyled += styleDup.Render(fmt.Sprintf(T("dup.suffix"), dupN))
	}

	if selected {
		titlePad := title + strings.Repeat(" ", contentW-runewidth.StringWidth(title))
		metaPad := metaPlain + strings.Repeat(" ", contentW-runewidth.StringWidth(metaPlain))
		bar := styleCursorBar.Render("▌ ")
		b.WriteString("  " + bar + styleSelectedTitle.Render(titlePad) + "\n")
		b.WriteString("  " + bar + styleSelectedBg.Render(metaPad) + "\n")
		if wrap {
			pad := badgesPlain + strings.Repeat(" ", contentW-runewidth.StringWidth(badgesPlain))
			b.WriteString("  " + bar + styleSelectedBg.Render(pad) + "\n")
			return 3
		}
		return 2
	}

	titleOut := title
	if !hasTitle {
		titleOut = styleDim.Render(title)
	}

	var metaOut string
	if metaTruncated {
		metaOut = styleDim.Render(metaPlain)
	} else {
		metaOut = fmt.Sprintf("%s %s %s %s %d %s",
			styleBranch.Render(branch),
			styleDim.Render("·"),
			styleDim.Render(humanizeAgo(s.LastActivity)),
			styleDim.Render("·"),
			s.MessageCount,
			styleDim.Render(T("msgs")),
		)
		if warn != "" {
			metaOut += "  " + styleWarn.Render("⚠ "+warn)
		}
		if !wrap && badgesStyled != "" {
			metaOut += "  " + badgesStyled
		}
	}

	b.WriteString("    " + titleOut + "\n")
	b.WriteString("    " + metaOut + "\n")
	if wrap {
		b.WriteString("    " + badgesStyled + "\n")
		return 3
	}
	return 2
}

// renderSubAgentRow writes a 2-line sub-agent card and returns the line
// count. Line 1 is the agent's own first user message (or "(no message)"
// fallback); line 2 lists agentType · description · messages · when.
func renderSubAgentRow(b *strings.Builder, a *SubAgent, selected bool, width int) int {
	contentW := width - 4
	if contentW < 20 {
		contentW = 20
	}

	title := a.FirstMessage
	hasTitle := title != ""
	if !hasTitle {
		title = T("no_message")
	}
	if runewidth.StringWidth(title) > contentW {
		title = runewidth.Truncate(title, contentW, "…")
	}

	at := a.AgentType
	if at == "" {
		at = "agent"
	}
	desc := a.Description
	metaParts := []string{at}
	if desc != "" {
		metaParts = append(metaParts, desc)
	}
	metaParts = append(metaParts,
		fmt.Sprintf("%d %s", a.MessageCount, T("msgs")),
		humanizeAgo(a.LastActivity),
	)
	metaPlain := strings.Join(metaParts, " · ")
	if runewidth.StringWidth(metaPlain) > contentW {
		metaPlain = runewidth.Truncate(metaPlain, contentW, "…")
	}

	if selected {
		titlePad := title + strings.Repeat(" ", contentW-runewidth.StringWidth(title))
		metaPad := metaPlain + strings.Repeat(" ", contentW-runewidth.StringWidth(metaPlain))
		bar := styleCursorBar.Render("▌ ")
		b.WriteString("  " + bar + styleSelectedTitle.Render(titlePad) + "\n")
		b.WriteString("  " + bar + styleSelectedBg.Render(metaPad) + "\n")
		return 2
	}

	titleOut := title
	if !hasTitle {
		titleOut = styleDim.Render(title)
	}
	b.WriteString("    " + titleOut + "\n")
	b.WriteString("    " + styleDim.Render(metaPlain) + "\n")
	return 2
}

func humanizeAgo(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return T("time.just_now")
	case d < time.Hour:
		return fmt.Sprintf(T("time.m_ago"), int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf(T("time.h_ago"), int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf(T("time.d_ago"), int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}
