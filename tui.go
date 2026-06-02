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
	styleHelp           = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleSearchLabel    = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	styleScrollHint     = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
)

// ---------- model ----------

// row represents one rendered entry — either a group header, a session, or a
// "more" toggle that expands/collapses the project's remaining sessions.
type row struct {
	isGroup bool
	isMore  bool
	group   string   // for group/more rows: the project name
	session *Session // for session rows
	warn    string
	hiddenN int // for more rows: count of hidden sessions (0 means "collapse" toggle)
}

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

	Selected *Session
	Quit     bool
}

func NewModel(sessions []Session, currentCWD string) Model {
	ti := textinput.New()
	ti.Placeholder = T("filter.placeholder")
	ti.Prompt = ""
	ti.CharLimit = 200

	vp := viewport.New(0, 0)

	m := Model{
		all:        sessions,
		search:     ti,
		currentCWD: currentCWD,
		vp:         vp,
	}
	m.rebuildRows("")
	m.cursorToFirstSession()
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

// rebuildRows applies the current search filter and groups by project.
func (m *Model) rebuildRows(query string) {
	filtered := m.all
	if q := strings.TrimSpace(query); q != "" {
		hay := make([]string, len(m.all))
		for i, s := range m.all {
			hay[i] = s.Project + " " + s.FirstMessage
		}
		matches := fuzzy.Find(q, hay)
		filtered = make([]Session, 0, len(matches))
		for _, mt := range matches {
			filtered = append(filtered, m.all[mt.Index])
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
		m.rows = append(m.rows, row{isGroup: true, group: g.name})
		visible := len(g.sessions)
		if cap > 0 && visible > cap {
			visible = cap
		}
		for i := 0; i < visible; i++ {
			s := g.sessions[i]
			m.rows = append(m.rows, row{session: &s, warn: computeWarn(s)})
		}
		if hidden := len(g.sessions) - visible; hidden > 0 {
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

// totalSessions returns the count of session rows in the current (possibly filtered) view.
func (m Model) totalSessions() int {
	c := 0
	for _, r := range m.rows {
		if !r.isGroup {
			c++
		}
	}
	return c
}

// cursorSessionIndex returns 1-based position of the cursor among session rows.
func (m Model) cursorSessionIndex() int {
	c := 0
	for i, r := range m.rows {
		if !r.isGroup {
			c++
		}
		if i == m.cursor {
			return c
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
		renderSessionRow(&b, r.session, r.warn, i == m.cursor, width)
		line += 2
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
	// row height: session rows are 2 lines, more-row is 1.
	rowHeight := 2
	if m.rows[m.cursor].isMore {
		rowHeight = 1
	}
	cursorTop := m.rowLines[m.cursor]
	cursorBottom := cursorTop + rowHeight - 1

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

		switch msg.String() {
		case "q", "ctrl+c":
			m.Quit = true
			return m, tea.Quit
		case "esc":
			// ESC inside drill-down returns to the overview, only quits at top.
			if m.drillProject != "" {
				m.drillProject = ""
				m.rebuildRows("")
				m.cursorToFirstSession()
				m.rebuildContent()
				m.scrollToCursor()
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
	if total != len(m.all) {
		counter += "  " + fmt.Sprintf(T("of_total"), len(m.all))
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
	rightLines := []string{
		"",
		styleSearchLabel.Render(T("header.csm")) + "  " + styleVersion.Render("v"+Version),
		styleTagline.Render("Claude Code session manager"),
		styleDim.Render(counter + " sessions"),
		styleHelp.Render(T("header.keys1")),
		styleHelp.Render(T("header.keys2")),
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
	var b strings.Builder

	// header
	b.WriteString(m.renderHeader())

	// scrollable viewport
	b.WriteString(m.vp.View())

	// footer — single line, scroll indicator only (or filter-mode help)
	if m.filtering {
		b.WriteString(styleHelp.Render(T("footer.filter")))
	} else {
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

// renderSessionRow writes a 2-line session card. selected rows get a colored left
// bar + filled background on both lines.
func renderSessionRow(b *strings.Builder, s *Session, warn string, selected bool, width int) {
	contentW := width - 4
	if contentW < 20 {
		contentW = 20
	}

	title := s.FirstMessage
	hasTitle := title != ""
	if !hasTitle {
		title = T("no_message")
	}
	if runewidth.StringWidth(title) > contentW {
		title = runewidth.Truncate(title, contentW, "…")
	}

	branch := s.GitBranch
	if branch == "" {
		branch = "—"
	}
	metaPlain := fmt.Sprintf("%s · %s · %d %s", branch, humanizeAgo(s.LastActivity), s.MessageCount, T("msgs"))
	if warn != "" {
		metaPlain += "  ⚠ " + warn
	}
	metaTruncated := false
	if runewidth.StringWidth(metaPlain) > contentW {
		metaPlain = runewidth.Truncate(metaPlain, contentW, "…")
		metaTruncated = true
	}

	if selected {
		titlePad := title + strings.Repeat(" ", contentW-runewidth.StringWidth(title))
		metaPad := metaPlain + strings.Repeat(" ", contentW-runewidth.StringWidth(metaPlain))
		bar := styleCursorBar.Render("▌ ")
		b.WriteString("  " + bar + styleSelectedTitle.Render(titlePad) + "\n")
		b.WriteString("  " + bar + styleSelectedBg.Render(metaPad) + "\n")
		return
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
	}

	b.WriteString("    " + titleOut + "\n")
	b.WriteString("    " + metaOut + "\n")
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
