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

// DryRun renders what a command run with --dry-run would have done.
func DryRun(created, modified, manual []string) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(styleTitle.Render("🤠 Dry run: nothing was written"))
	b.WriteString("\n\n")
	if len(created)+len(modified) == 0 {
		b.WriteString("No files would change.\n")
	}
	if len(created) > 0 {
		b.WriteString("It would create:\n\n")
		for _, path := range created {
			fmt.Fprintf(&b, "  %s %s\n", styleGood.Render("+"), path)
		}
		b.WriteString("\n")
	}
	if len(modified) > 0 {
		b.WriteString("It would change:\n\n")
		for _, path := range modified {
			fmt.Fprintf(&b, "  %s %s\n", styleWarn.Render("~"), path)
		}
		b.WriteString("\n")
	}
	for _, m := range manual {
		b.WriteString(styleWarn.Render("Needs a hand: "+m) + "\n")
	}
	if len(manual) > 0 {
		b.WriteString("\n")
	}
	return b.String()
}

// Outcome is how a check went, for CheckLine.
type Outcome int

const (
	Passed Outcome = iota
	Failed
	Skipped
)

// CheckLine renders one line of 'howdy check': an icon, the check's name
// and a short detail.
func CheckLine(outcome Outcome, name, detail string) string {
	name = fmt.Sprintf("%-6s", name)
	switch outcome {
	case Passed:
		return fmt.Sprintf("  %s %s  %s", styleGood.Render("✓"), styleStep.Render(name), styleDetail.Render(detail))
	case Failed:
		return fmt.Sprintf("  %s %s  %s", styleBad.Render("✗"), styleBad.Render(name), styleDetail.Render(detail))
	}
	return fmt.Sprintf("  %s %s  %s", stylePending.Render("–"), stylePending.Render(name), styleDetail.Render(detail))
}

// Title renders a command's heading.
func Title(text string) string {
	return styleTitle.Render("🤠 " + text)
}
