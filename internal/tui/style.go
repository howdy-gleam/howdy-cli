// Package tui draws the animated progress for howdy commands.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorAccent = lipgloss.Color("#F5A623") // howdy orange
	colorGood   = lipgloss.Color("#3FB950")
	colorWarn   = lipgloss.Color("#D29922")
	colorBad    = lipgloss.Color("#F85149")
	colorDim    = lipgloss.Color("#8B949E")

	styleTitle   = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	styleStep    = lipgloss.NewStyle()
	stylePending = lipgloss.NewStyle().Foreground(colorDim)
	styleDetail  = lipgloss.NewStyle().Foreground(colorDim)
	styleGood    = lipgloss.NewStyle().Foreground(colorGood)
	styleWarn    = lipgloss.NewStyle().Foreground(colorWarn)
	styleBad     = lipgloss.NewStyle().Foreground(colorBad).Bold(true)
	styleCmd     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58A6FF"))
	styleSpinner = lipgloss.NewStyle().Foreground(colorAccent)
)

// ErrorLine formats an error for stderr.
func ErrorLine(msg string) string {
	return styleBad.Render("✗ ") + msg
}

// Command is a suggested next command with a short note.
type Command struct {
	Cmd  string
	Note string
}

// NextSteps renders the closing message after a project was created.
func NextSteps(name string, commands []Command, notes []string) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(styleTitle.Render(fmt.Sprintf("🤠 Howdy, %s is ready!", name)))
	b.WriteString("\n\n")
	b.WriteString("What to do next:\n\n")
	width := 0
	for _, c := range commands {
		if len(c.Cmd) > width {
			width = len(c.Cmd)
		}
	}
	for _, c := range commands {
		pad := strings.Repeat(" ", width-len(c.Cmd)+3)
		fmt.Fprintf(&b, "  %s%s%s\n", styleCmd.Render(c.Cmd), pad, styleDetail.Render(c.Note))
	}
	b.WriteString("\n")
	for _, n := range notes {
		b.WriteString(styleDetail.Render(n) + "\n")
	}
	b.WriteString("\n")
	return b.String()
}
