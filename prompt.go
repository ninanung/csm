package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// branchChoice represents the user's answer to "the session's branch doesn't exist".
//   Action == "stay":     continue on the current branch (no checkout).
//   Action == "checkout": checkout Branch.
//   Action == "abort":    exit without launching claude.
type branchChoice struct {
	Action string
	Branch string
}

// ---------- a tiny bubbletea picker reused for both action and branch selection ----------

type pickItem struct {
	key   string // identifier returned when selected
	label string // displayed text
}

type pickModel struct {
	title   string
	items   []pickItem
	cursor  int
	chosen  string
	aborted bool
}

func (m pickModel) Init() tea.Cmd { return nil }

func (m pickModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseButtonWheelDown:
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.aborted = true
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "g", "home":
			m.cursor = 0
		case "G", "end":
			if len(m.items) > 0 {
				m.cursor = len(m.items) - 1
			}
		case "enter":
			if len(m.items) > 0 {
				m.chosen = m.items[m.cursor].key
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m pickModel) View() string {
	var b strings.Builder
	if m.title != "" {
		b.WriteString(m.title)
		b.WriteString("\n\n")
	}
	for i, it := range m.items {
		if i == m.cursor {
			b.WriteString("  ")
			b.WriteString(styleCursorBar.Render("▌ "))
			b.WriteString(styleSelectedBg.Render(" " + it.label + " "))
			b.WriteString("\n")
		} else {
			b.WriteString("    " + it.label + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(styleHelp.Render(T("footer.pick")))
	return b.String()
}

func runPick(title string, items []pickItem) (chosen string, aborted bool) {
	if len(items) == 0 {
		return "", true
	}
	m := pickModel{title: title, items: items}
	prog := tea.NewProgram(m, tea.WithOutput(os.Stderr), tea.WithMouseCellMotion())
	final, err := prog.Run()
	if err != nil {
		return "", true
	}
	fm := final.(pickModel)
	if fm.aborted {
		return "", true
	}
	return fm.chosen, false
}

// ---------- public prompts ----------

// promptMissingBranch shows a picker when the session's recorded branch is missing.
func promptMissingBranch(missing, current string, available []string) branchChoice {
	title := styleWarn.Render(fmt.Sprintf(T("branch.title"), missing)) + "\n" +
		styleDim.Render(fmt.Sprintf(T("branch.current_line"), current))

	items := []pickItem{
		{"stay", fmt.Sprintf(T("branch.opt_stay"), current)},
	}
	if len(available) > 0 {
		items = append(items, pickItem{"pick", T("branch.opt_pick")})
	}
	items = append(items, pickItem{"abort", T("branch.opt_abort")})

	chosen, aborted := runPick(title, items)
	if aborted {
		return branchChoice{Action: "abort"}
	}
	switch chosen {
	case "stay", "":
		return branchChoice{Action: "stay"}
	case "abort":
		return branchChoice{Action: "abort"}
	case "pick":
		return pickBranchFromList(available, current)
	}
	return branchChoice{Action: "stay"}
}

func pickBranchFromList(branches []string, current string) branchChoice {
	title := styleSearchLabel.Render(T("branch.pick_title"))
	items := make([]pickItem, 0, len(branches))
	for _, name := range branches {
		label := name
		if name == current {
			label = name + styleDim.Render(T("branch.current_marker"))
		}
		items = append(items, pickItem{name, label})
	}
	chosen, aborted := runPick(title, items)
	if aborted || chosen == "" || chosen == current {
		return branchChoice{Action: "stay"}
	}
	return branchChoice{Action: "checkout", Branch: chosen}
}
