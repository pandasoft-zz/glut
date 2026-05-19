package reporter

import (
	"io"

	"github.com/charmbracelet/lipgloss"
)

const (
	passEmoji = "✓"
	failEmoji = "✗"
	logoEmoji = "🧪"
)

type consoleStyles struct {
	pass   lipgloss.Style
	fail   lipgloss.Style
	dim    lipgloss.Style
	counter lipgloss.Style
	logoGL lipgloss.Style
	logoUT lipgloss.Style
	path   lipgloss.Style
	dur    lipgloss.Style
	jobSep lipgloss.Style
	jobOut lipgloss.Style
}

func newConsoleStyles(w io.Writer) consoleStyles {
	r := lipgloss.NewRenderer(w)
	return consoleStyles{
		pass:    r.NewStyle().Foreground(lipgloss.Color("10")).Bold(true),
		fail:    r.NewStyle().Foreground(lipgloss.Color("9")).Bold(true),
		dim:     r.NewStyle().Foreground(lipgloss.Color("8")),
		counter: r.NewStyle().Foreground(lipgloss.Color("8")),
		logoGL:  r.NewStyle().Foreground(lipgloss.Color("214")).Bold(true),
		logoUT:  r.NewStyle().Foreground(lipgloss.Color("15")).Bold(true),
		path:    r.NewStyle().Foreground(lipgloss.Color("7")),
		dur:     r.NewStyle().Foreground(lipgloss.Color("8")),
		jobSep:  r.NewStyle().Foreground(lipgloss.Color("6")),
		jobOut:  r.NewStyle().Foreground(lipgloss.Color("7")),
	}
}
