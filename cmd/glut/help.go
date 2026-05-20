package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// helpSt holds lipgloss styles for the rich help output.
type helpSt struct {
	logoGL  lipgloss.Style
	logoUT  lipgloss.Style
	section lipgloss.Style
	name    lipgloss.Style
	flag    lipgloss.Style
	dim     lipgloss.Style
	code    lipgloss.Style
}

func newHelpSt(w io.Writer) helpSt {
	r := lipgloss.NewRenderer(w)
	return helpSt{
		logoGL:  r.NewStyle().Foreground(lipgloss.Color("214")).Bold(true),
		logoUT:  r.NewStyle().Foreground(lipgloss.Color("15")).Bold(true),
		section: r.NewStyle().Foreground(lipgloss.Color("214")).Bold(true),
		name:    r.NewStyle().Foreground(lipgloss.Color("15")).Bold(true),
		flag:    r.NewStyle().Foreground(lipgloss.Color("6")),
		dim:     r.NewStyle().Foreground(lipgloss.Color("8")),
		code:    r.NewStyle().Foreground(lipgloss.Color("7")),
	}
}

func init() {
	// Set once on root; cobra traverses up the tree so all subcommands
	// that don't override will use this renderer automatically.
	rootCmd.SetHelpFunc(helpFunc)
}

func helpFunc(cmd *cobra.Command, _ []string) {
	renderHelp(cmd, cmd.OutOrStdout())
}

// helpf writes to w, discarding the error (write-to-help-output errors are
// unrecoverable and intentionally ignored, matching the reporter.writef pattern).
func helpf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// renderHelp writes styled help for cmd to w.
func renderHelp(cmd *cobra.Command, w io.Writer) {
	st := newHelpSt(w)

	// ── Header ────────────────────────────────────────────────────────────────
	logo := "🧪 " + st.logoGL.Render("GL") + st.logoUT.Render("UT")
	helpf(w, "%s  %s\n", logo, cmd.Short)

	// ── Long description ──────────────────────────────────────────────────────
	if long := strings.TrimSpace(cmd.Long); long != "" {
		lines := strings.Split(long, "\n")
		// Skip leading line if it only repeats the Short summary.
		start := 0
		if strings.TrimSpace(lines[0]) == strings.TrimSpace(cmd.Short) {
			start = 1
		}
		helpf(w, "\n")
		for _, line := range lines[start:] {
			if strings.TrimSpace(line) == "" {
				helpf(w, "\n")
			} else {
				helpf(w, "  %s\n", st.dim.Render(line))
			}
		}
	}

	// ── Usage ─────────────────────────────────────────────────────────────────
	useline := cmd.UseLine()
	if cmd.HasAvailableSubCommands() {
		// Replace cobra's auto-appended [flags] with the more useful [command].
		useline = strings.TrimSuffix(useline, " [flags]")
		useline += " [command]"
	}
	helpf(w, "\n%s\n", st.section.Render("USAGE"))
	helpf(w, "  %s\n", useline)

	// ── Subcommands ───────────────────────────────────────────────────────────
	var available []*cobra.Command
	for _, c := range cmd.Commands() {
		if c.IsAvailableCommand() {
			available = append(available, c)
		}
	}
	if len(available) > 0 {
		maxLen := 0
		for _, c := range available {
			if n := len(c.Name()); n > maxLen {
				maxLen = n
			}
		}
		helpf(w, "\n%s\n", st.section.Render("COMMANDS"))
		for _, c := range available {
			helpf(w, "  %s  %s\n",
				st.name.Render(fmt.Sprintf("%-*s", maxLen, c.Name())),
				c.Short,
			)
		}
	}

	// ── Flags ─────────────────────────────────────────────────────────────────
	if cmd.HasAvailableFlags() {
		helpf(w, "\n%s\n", st.section.Render("FLAGS"))
		renderFlags(w, st, cmd.Flags())
	}

	// ── Examples ──────────────────────────────────────────────────────────────
	if cmd.Example != "" {
		helpf(w, "\n%s\n", st.section.Render("EXAMPLES"))
		for _, line := range strings.Split(strings.TrimRight(cmd.Example, "\n"), "\n") {
			helpf(w, "%s\n", st.code.Render(line))
		}
	}

	// ── Footer ────────────────────────────────────────────────────────────────
	if len(available) > 0 {
		helpf(w, "\n%s\n",
			st.dim.Render(fmt.Sprintf(`Use "%s [command] --help" for more information about a command.`, cmd.CommandPath())))
	}
}

// renderFlags writes a two-column flag table (syntax | description) to w.
func renderFlags(w io.Writer, st helpSt, fs *pflag.FlagSet) {
	type entry struct {
		syntax string
		usage  string
		def    string
	}

	var entries []entry
	maxSyntax := 0

	fs.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}

		tl := helpFlagType(f)
		var syntax string
		if f.Shorthand != "" && f.ShorthandDeprecated == "" {
			if tl == "" {
				syntax = fmt.Sprintf("-%s, --%s", f.Shorthand, f.Name)
			} else {
				syntax = fmt.Sprintf("-%s, --%s %s", f.Shorthand, f.Name, tl)
			}
		} else {
			if tl == "" {
				syntax = fmt.Sprintf("    --%s", f.Name)
			} else {
				syntax = fmt.Sprintf("    --%s %s", f.Name, tl)
			}
		}

		var def string
		if helpShowDefault(f) {
			def = fmt.Sprintf(" (default %q)", f.DefValue)
		}

		if len(syntax) > maxSyntax {
			maxSyntax = len(syntax)
		}
		entries = append(entries, entry{syntax: syntax, usage: f.Usage, def: def})
	})

	for _, e := range entries {
		helpf(w, "  %s  %s%s\n",
			st.flag.Render(fmt.Sprintf("%-*s", maxSyntax, e.syntax)),
			e.usage,
			st.dim.Render(e.def),
		)
	}
}

// helpFlagType returns a short human-friendly type label for a flag.
// Bool flags return "" so the type is omitted from the syntax column.
func helpFlagType(f *pflag.Flag) string {
	switch f.Value.Type() {
	case "bool":
		return ""
	case "stringArray", "stringSlice":
		return "strings"
	default:
		return f.Value.Type()
	}
}

// helpShowDefault reports whether the flag's default value is worth printing.
func helpShowDefault(f *pflag.Flag) bool {
	switch f.Value.Type() {
	case "bool":
		return f.DefValue != "false"
	case "int", "int64", "uint", "uint64", "float64":
		return f.DefValue != "0"
	case "string":
		return f.DefValue != ""
	case "stringArray", "stringSlice":
		return f.DefValue != "[]" && f.DefValue != ""
	default:
		return f.DefValue != ""
	}
}
