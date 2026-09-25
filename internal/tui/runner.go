package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

// ErrCancelled is returned when the user interrupts the run.
var ErrCancelled = errors.New("cancelled")

// Reporter lets a step describe its progress while it runs.
type Reporter interface {
	// Detail sets the short text shown next to the running step.
	Detail(text string)
	// Log adds a persistent line under the step.
	Log(line string)
	// Warn adds a persistent warning under the step.
	Warn(line string)
}

// Step is one unit of work with a title shown while it runs.
type Step struct {
	Title string
	// Skip, when set and returning true, marks the step as skipped with
	// the given reason instead of running it.
	Skip func() (reason string, skip bool)
	// Run does the work and returns a short summary for the finished line.
	Run func(r Reporter) (summary string, err error)
}

// Run executes the steps in order, animating them on a terminal or printing
// plain lines when plain is set or stdout is not a terminal. It stops at
// the first failing step and returns its error.
func Run(title string, steps []Step, plain bool) error {
	if plain || !isatty.IsTerminal(os.Stdout.Fd()) {
		return runPlain(title, steps)
	}
	return runAnimated(title, steps)
}

// -- Plain output ------------------------------------------------------------

type plainReporter struct{}

func (plainReporter) Detail(string)    {}
func (plainReporter) Log(line string)  { fmt.Println("    " + line) }
func (plainReporter) Warn(line string) { fmt.Println("    ! " + line) }

func runPlain(title string, steps []Step) error {
	fmt.Println(title)
	for _, s := range steps {
		if s.Skip != nil {
			if reason, skip := s.Skip(); skip {
				fmt.Printf("  - %s: %s\n", s.Title, reason)
				continue
			}
		}
		fmt.Printf("  * %s\n", s.Title)
		summary, err := s.Run(plainReporter{})
		if err != nil {
			fmt.Printf("  x %s failed\n", s.Title)
			return err
		}
		fmt.Printf("  + %s: %s\n", s.Title, summary)
	}
	return nil
}

// -- Animated output ---------------------------------------------------------

type status int

const (
	pending status = iota
	running
	done
	skipped
	failed
)

type stepState struct {
	title   string
	status  status
	detail  string
	summary string
	lines   []line
}

type line struct {
	text string
	warn bool
}

type (
	msgStarted struct{ index int }
	msgDetail  struct {
		index int
		text  string
	}
	msgLog struct {
		index int
		text  string
		warn  bool
	}
	msgFinished struct {
		index   int
		summary string
		status  status
	}
	msgFailed struct {
		index int
		err   error
	}
	msgAllDone struct{}
)

type model struct {
	title   string
	steps   []stepState
	spinner spinner.Model
	err     error
	quit    bool
}

type programReporter struct {
	p     *tea.Program
	index int
}

func (r programReporter) Detail(text string) { r.p.Send(msgDetail{r.index, text}) }
func (r programReporter) Log(text string)    { r.p.Send(msgLog{r.index, text, false}) }
func (r programReporter) Warn(text string)   { r.p.Send(msgLog{r.index, text, true}) }

func runAnimated(title string, steps []Step) error {
	m := model{title: title, spinner: spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(styleSpinner))}
	for _, s := range steps {
		m.steps = append(m.steps, stepState{title: s.Title})
	}
	p := tea.NewProgram(m)

	go func() {
		for i, s := range steps {
			if s.Skip != nil {
				if reason, skip := s.Skip(); skip {
					p.Send(msgFinished{i, reason, skipped})
					continue
				}
			}
			p.Send(msgStarted{i})
			summary, err := s.Run(programReporter{p, i})
			if err != nil {
				p.Send(msgFailed{i, err})
				return
			}
			p.Send(msgFinished{i, summary, done})
		}
		p.Send(msgAllDone{})
	}()

	final, err := p.Run()
	if err != nil {
		return err
	}
	fm := final.(model)
	if fm.err != nil {
		return fm.err
	}
	if fm.quit {
		return ErrCancelled
	}
	return nil
}

func (m model) Init() tea.Cmd { return m.spinner.Tick }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.String() == "q" {
			m.quit = true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case msgStarted:
		m.steps[msg.index].status = running
	case msgDetail:
		m.steps[msg.index].detail = msg.text
	case msgLog:
		m.steps[msg.index].lines = append(m.steps[msg.index].lines, line{msg.text, msg.warn})
	case msgFinished:
		s := &m.steps[msg.index]
		s.status, s.summary, s.detail = msg.status, msg.summary, ""
	case msgFailed:
		s := &m.steps[msg.index]
		s.status, s.detail = failed, ""
		m.err = msg.err
		return m, tea.Quit
	case msgAllDone:
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString("\n" + styleTitle.Render("🤠 "+m.title) + "\n\n")
	for _, s := range m.steps {
		var icon, text string
		switch s.status {
		case pending:
			icon = stylePending.Render("○")
			text = stylePending.Render(s.title)
		case running:
			icon = m.spinner.View()
			text = styleStep.Render(s.title)
			if s.detail != "" {
				text += "  " + styleDetail.Render(s.detail)
			}
		case done:
			icon = styleGood.Render("✓")
			text = styleStep.Render(s.title) + "  " + styleDetail.Render(s.summary)
		case skipped:
			icon = stylePending.Render("–")
			text = stylePending.Render(s.title) + "  " + styleDetail.Render(s.summary)
		case failed:
			icon = styleBad.Render("✗")
			text = styleBad.Render(s.title)
		}
		fmt.Fprintf(&b, "  %s %s\n", icon, text)
		for _, l := range s.lines {
			if l.warn {
				fmt.Fprintf(&b, "      %s %s\n", styleWarn.Render("!"), styleWarn.Render(l.text))
			} else {
				fmt.Fprintf(&b, "      %s %s\n", styleGood.Render("+"), styleDetail.Render(l.text))
			}
		}
	}
	if m.err != nil {
		b.WriteString("\n")
	}
	return b.String()
}
