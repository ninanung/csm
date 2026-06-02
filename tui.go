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

// row represents one rendered entry — either a group header or a session.
type row struct {
	isGroup bool
	group   string
	session *Session
	warn    string
}

const (
	headerLines = 2 // title line + blank line below
	footerLines = 2 // blank line + help line
)

type Model struct {
	all       []Session
	rows      []row
	cursor    int
	search    textinput.Model
	filtering bool

	currentCWD string

	width, height int

	vp        viewport.Model
	rowLines  []int  // line index in rendered content where each row begins
	totalLine int    // total line count of rendered content

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
	for _, g := range groups {
		m.rows = append(m.rows, row{isGroup: true, group: g.name})
		for i := range g.sessions {
			s := g.sessions[i]
			warn := computeWarn(s)
			m.rows = append(m.rows, row{session: &s, warn: warn})
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
// Each session occupies 2 lines starting at rowLines[cursor]; we keep both lines in view.
func (m *Model) scrollToCursor() {
	if m.cursor < 0 || m.cursor >= len(m.rowLines) {
		return
	}
	const rowHeight = 2
	top := m.rowLines[m.cursor]
	bottom := top + rowHeight - 1
	vpTop := m.vp.YOffset
	vpBottom := vpTop + m.vp.Height - 1

	if top < vpTop {
		m.vp.SetYOffset(top)
	} else if bottom > vpBottom {
		m.vp.SetYOffset(bottom - m.vp.Height + 1)
	}
}

func (m *Model) resize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.vp.Width = m.width
	h := m.height - headerLines - footerLines
	if h < 1 {
		h = 1
	}
	m.vp.Height = h
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
		case "q", "ctrl+c", "esc":
			m.Quit = true
			return m, tea.Quit
		case "enter":
			if s := m.currentSession(); s != nil {
				m.Selected = s
				return m, tea.Quit
			}
		case "j", "down":
			m.moveCursor(1)
		case "k", "up":
			m.moveCursor(-1)
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
			m.rebuildContent()
			m.scrollToCursor()
			return m, textinput.Blink
		}
		m.rebuildContent()
		m.scrollToCursor()
	}
	return m, nil
}

func (m Model) View() string {
	var b strings.Builder

	// header
	if m.filtering {
		b.WriteString(styleSearchLabel.Render("/ "))
		b.WriteString(m.search.View())
		b.WriteString("\n\n")
	} else {
		total := m.totalSessions()
		pos := m.cursorSessionIndex()
		counter := fmt.Sprintf("  %d / %d", pos, total)
		if total != len(m.all) {
			counter += "  " + fmt.Sprintf(T("of_total"), len(m.all))
		}
		b.WriteString(styleAccent.Render("◆ "))
		b.WriteString(styleSearchLabel.Render(T("header.csm")))
		b.WriteString(styleVersion.Render("  v" + Version))
		b.WriteString(styleDim.Render(counter))
		b.WriteString("\n\n")
	}

	// scrollable viewport
	b.WriteString(m.vp.View())

	// footer
	b.WriteString("\n")
	if m.filtering {
		b.WriteString(styleHelp.Render(T("footer.filter")))
	} else {
		// scroll indicators
		var above, below bool
		if m.vp.YOffset > 0 {
			above = true
		}
		if m.vp.YOffset+m.vp.Height < m.totalLine {
			below = true
		}
		b.WriteString(styleHelp.Render(T("footer.normal")))
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
			b.WriteString(styleScrollHint.Render("  " + T(key)))
		}
	}
	return b.String()
}

// ---------- session row rendering ----------

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
