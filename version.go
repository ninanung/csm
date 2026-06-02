package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Version is the current csm release. Bump on a real release.
const Version = "0.1.0"

const logoArt = `  ____ ____  __  __
 / ___/ ___||  \/  |
| |   \___ \| |\/| |
| |___ ___) | |  | |
 \____|____/|_|  |_|`

var (
	styleLogo    = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	styleVersion = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleTagline = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	styleCmd     = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	styleAccent  = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
)

// printSplash writes the project banner — logo, version, tagline, command list —
// to w. Used by `csm version` / `csm --version`.
func printSplash(w io.Writer) {
	fmt.Fprintln(w)
	lines := strings.Split(logoArt, "\n")
	for i, line := range lines {
		prefix := "  "
		suffix := ""
		if i == 0 {
			suffix = "   " + styleVersion.Render("v"+Version)
		}
		fmt.Fprintln(w, prefix+styleLogo.Render(line)+suffix)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  "+styleTagline.Render("Claude Code session manager"))
	fmt.Fprintln(w, "  "+styleVersion.Render("Browse and resume sessions with rich identification."))
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Commands:")
	cmds := []struct{ name, desc string }{
		{"csm", "interactive picker (default)"},
		{"csm --print", "print selection to stdout, no exec"},
		{"csm --lang <en|ko>", "force interface language"},
		{"csm version", "show this splash"},
		{"csm -h, --help", "full usage"},
	}
	for _, c := range cmds {
		fmt.Fprintf(w, "    %-22s %s\n", styleCmd.Render(c.name), styleVersion.Render(c.desc))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  "+styleVersion.Render("Auto-detects language from CSM_LANG / LC_ALL / LC_MESSAGES / LANG."))
	fmt.Fprintln(w)
}
