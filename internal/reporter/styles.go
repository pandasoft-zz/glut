package reporter

import (
	"io"

	"github.com/charmbracelet/lipgloss"
)

const (
	passEmoji = "✓"
	failEmoji = "✗"
	runEmoji  = "◆"
)

type consoleStyles struct {
	pass    lipgloss.Style
	fail    lipgloss.Style
	dim     lipgloss.Style
	counter lipgloss.Style
	header  lipgloss.Style
	path    lipgloss.Style
	dur     lipgloss.Style
	jobSep  lipgloss.Style
	jobOut  lipgloss.Style
}

func newConsoleStyles(w io.Writer) consoleStyles {
	r := lipgloss.NewRenderer(w)
	return consoleStyles{
		pass:    r.NewStyle().Foreground(lipgloss.Color("10")).Bold(true),
		fail:    r.NewStyle().Foreground(lipgloss.Color("9")).Bold(true),
		dim:     r.NewStyle().Foreground(lipgloss.Color("8")),
		counter: r.NewStyle().Foreground(lipgloss.Color("8")),
		header:  r.NewStyle().Foreground(lipgloss.Color("12")).Bold(true),
		path:    r.NewStyle().Foreground(lipgloss.Color("7")),
		dur:     r.NewStyle().Foreground(lipgloss.Color("8")),
		jobSep:  r.NewStyle().Foreground(lipgloss.Color("6")),
		jobOut:  r.NewStyle().Foreground(lipgloss.Color("7")),
	}
}
