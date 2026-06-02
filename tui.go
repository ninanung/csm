package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

// ---------- styles ----------

var (
	styleGroup       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	styleCursor      = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	styleSelected    = lipgloss.NewStyle().Background(lipgloss.Color("236")).Foreground(lipgloss.Color("15"))
	styleDim         = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleBranch      = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleBranchWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleWarn        = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleHelp        = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleSearchLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
)

// ---------- model ----------

// row represents one rendered line — either a group header or a session entry.
type row struct {
	isGroup bool
	group   string
	session *Session
	warn    string // appended warning marker
}

type Model struct {
	all      []Session // unfiltered, sorted by recency
	rows     []row     // current rendered rows after filter/group
	cursor   int       // index into rows; only session rows are selectable
	search   textinput.Model
	filtering bool

	currentCWD string

	width, height int

	Selected *Session // set when user confirms; nil if cancelled
	Quit     bool     // user cancelled
}

func NewModel(sessions []Session, currentCWD string) Model {
	ti := textinput.New()
	ti.Placeholder = "type to filter…"
	ti.Prompt = ""
	ti.CharLimit = 200

	m := Model{
		all:        sessions,
		search:     ti,
		currentCWD: currentCWD,
	}
	m.rebuildRows("")
	m.cursorToFirstSession()
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

// rebuildRows applies search filter and groups by project.
func (m *Model) rebuildRows(query string) {
	filtered := m.all
	if q := strings.TrimSpace(query); q != "" {
		// fuzzy on a "haystack" of "project + first message"
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

	// group: current project first (expanded), then others by recency of latest activity
	byProject := map[string][]Session{}
	for _, s := range filtered {
		byProject[s.Project] = append(byProject[s.Project], s)
	}

	type grp struct {
		name      string
		sessions  []Session
		latest    time.Time
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
		// current project always first
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
	// minimal warnings checked from session metadata alone (no git call here per row)
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

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// when filtering, most keys go to the search box; specific keys still navigate
		if m.filtering {
			switch msg.Type {
			case tea.KeyEsc:
				m.filtering = false
				m.search.Blur()
				m.search.SetValue("")
				m.rebuildRows("")
				m.cursorToFirstSession()
				return m, nil
			case tea.KeyEnter:
				if s := m.currentSession(); s != nil {
					m.Selected = s
					return m, tea.Quit
				}
				return m, nil
			case tea.KeyUp, tea.KeyCtrlP:
				m.moveCursor(-1)
				return m, nil
			case tea.KeyDown, tea.KeyCtrlN:
				m.moveCursor(1)
				return m, nil
			}
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(msg)
			m.rebuildRows(m.search.Value())
			// keep cursor on a session row
			if m.cursor >= len(m.rows) || (m.cursor < len(m.rows) && m.rows[m.cursor].isGroup) {
				m.cursorToFirstSession()
			}
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
		case "g":
			m.cursorToFirstSession()
		case "G":
			for i := len(m.rows) - 1; i >= 0; i-- {
				if !m.rows[i].isGroup {
					m.cursor = i
					break
				}
			}
		case "/":
			m.filtering = true
			m.search.Focus()
			return m, textinput.Blink
		}
	}
	return m, nil
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
	// no movement if hit end
}

func (m Model) currentSession() *Session {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return m.rows[m.cursor].session
}

func (m Model) View() string {
	var b strings.Builder

	// header / search
	if m.filtering {
		b.WriteString(styleSearchLabel.Render("/ "))
		b.WriteString(m.search.View())
		b.WriteString("\n\n")
	} else {
		b.WriteString(styleSearchLabel.Render("csm"))
		b.WriteString(styleDim.Render(fmt.Sprintf("  %d sessions", len(m.all))))
		b.WriteString("\n\n")
	}

	// list
	for i, r := range m.rows {
		if r.isGroup {
			b.WriteString(styleGroup.Render("▸ " + r.group))
			b.WriteString("\n")
			continue
		}
		s := r.session

		cursor := "  "
		line := formatSession(s, r.warn)
		if i == m.cursor {
			cursor = styleCursor.Render("❯ ")
			line = styleSelected.Render(line)
		}
		b.WriteString("  " + cursor + line + "\n")
	}

	// footer / help
	b.WriteString("\n")
	if m.filtering {
		b.WriteString(styleHelp.Render("↑/↓ navigate · enter select · esc cancel filter"))
	} else {
		b.WriteString(styleHelp.Render("↑/↓ or j/k · enter select · / filter · q quit"))
	}
	return b.String()
}

func formatSession(s *Session, warn string) string {
	msg := s.FirstMessage
	if msg == "" {
		msg = styleDim.Render("(no message)")
	}
	if len(msg) > 70 {
		msg = msg[:67] + "..."
	}

	branch := s.GitBranch
	if branch == "" {
		branch = "—"
	}

	ago := humanizeAgo(s.LastActivity)

	var warnPart string
	if warn != "" {
		warnPart = " " + styleWarn.Render("⚠ "+warn)
	}

	return fmt.Sprintf("%s  %s  %s%s",
		msg,
		styleBranch.Render(branch),
		styleDim.Render(ago),
		warnPart,
	)
}

func humanizeAgo(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}
