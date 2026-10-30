// Package searchbar wraps a bubbles textinput for diff searching, styled
// after glow's stash filter input.
package searchbar

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/ui/styles"
)

// Model is the diff search input.
type Model struct {
	input  textinput.Model
	active bool
	styles styles.Styles
}

// New builds the search bar.
func New(st styles.Styles) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "search in diff…"
	ti.CharLimit = 512
	return Model{input: ti, styles: st}
}

// Active reports whether the search bar has focus.
func (m Model) Active() bool { return m.active }

// Value returns the current search text.
func (m Model) Value() string { return m.input.Value() }

// Activate focuses the input and returns the focus command.
func (m *Model) Activate() tea.Cmd {
	m.active = true
	return m.input.Focus()
}

// Deactivate blurs the input but keeps its text, so reopening the search
// picks the last query back up.
func (m *Model) Deactivate() {
	m.active = false
	m.input.Blur()
}

// Clear empties the query.
func (m *Model) Clear() { m.input.SetValue("") }

// SetStyles updates the theme.
func (m *Model) SetStyles(st styles.Styles) { m.styles = st }

// SetWidth sizes the input.
func (m *Model) SetWidth(width int) { m.input.SetWidth(max(1, width-4)) }

// Update processes input messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View renders the search input with glow's yellow-green prompt.
func (m Model) View(width int, matchMsg string) string {
	if width < 1 {
		return ""
	}
	count := ""
	if matchMsg != "" {
		// The count shares the muted tone of the rest of the line, so a live
		// search reads as one quiet strip instead of a green callout.
		count = " " + m.styles.SemiDimGreenFg(matchMsg)
	}

	// Reserve the count's cells out of the input so both always fit on the
	// one row the pane gives the bar.
	in := m.input
	// One extra cell for the input's cursor, which the textinput draws
	// without counting in its width.
	in.SetWidth(max(4, width-4-ansi.StringWidth(count)))

	bar := ansi.Truncate(m.styles.Subtle().Render("  ")+in.View()+count, width, "…")
	if pad := width - ansi.StringWidth(bar); pad > 0 {
		bar += strings.Repeat(" ", pad)
	}
	return bar
}
