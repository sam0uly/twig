// Package composer renders the commit message editor, a bubbles textarea
// shown inside the commit dialog.
package composer

import (
	"image/color"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"twig/internal/ui/styles"
)

// Model is the commit message editor.
type Model struct {
	ta     textarea.Model
	styles styles.Styles
}

// New builds the composer.
func New(st styles.Styles) Model {
	ta := textarea.New()
	ta.Placeholder = "Describe this change…"
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	// No left border: the dialog frames the editor itself.
	ta.Prompt = ""
	return Model{ta: ta, styles: st}
}

// SetStyles updates the theme.
func (m *Model) SetStyles(st styles.Styles) { m.styles = st }

// SetBackground paints the editor's background. Every text style the textarea
// emits gets the same background, so typed text never punches transparent
// holes in the dialog panel.
func (m *Model) SetBackground(c color.Color) {
	if c == nil {
		return
	}
	st := m.ta.Styles()
	paint := func(s *textarea.StyleState) {
		s.Base = s.Base.Background(c)
		s.Text = s.Text.Background(c)
		s.LineNumber = s.LineNumber.Background(c)
		s.CursorLineNumber = s.CursorLineNumber.Background(c)
		s.CursorLine = s.CursorLine.Background(c)
		s.EndOfBuffer = s.EndOfBuffer.Background(c)
		s.Placeholder = s.Placeholder.Background(c)
		s.Prompt = s.Prompt.Background(c)
		s.Selection = s.Selection.Background(c)
	}
	paint(&st.Focused)
	paint(&st.Blurred)
	m.ta.SetStyles(st)
}

// SetEditorSize sizes the textarea exactly, in cells.
func (m *Model) SetEditorSize(width, height int) {
	m.ta.SetWidth(max(1, width))
	m.ta.SetHeight(max(1, height))
}

// SetCursorLineColor repaints the highlight behind the line holding the
// cursor, so it matches the dialog instead of the textarea default.
func (m *Model) SetCursorLineColor(c color.Color) {
	if c == nil {
		return
	}
	st := m.ta.Styles()
	st.Focused.CursorLine = st.Focused.CursorLine.Background(c)
	st.Blurred.CursorLine = st.Blurred.CursorLine.Background(c)
	m.ta.SetStyles(st)
}

// SetSelectionColors repaints selected text.
func (m *Model) SetSelectionColors(bg, fg color.Color) {
	st := m.ta.Styles()
	paint := func(s *textarea.StyleState) {
		if bg != nil {
			s.Selection = s.Selection.Background(bg)
		}
		if fg != nil {
			s.Selection = s.Selection.Foreground(fg)
		}
	}
	paint(&st.Focused)
	paint(&st.Blurred)
	m.ta.SetStyles(st)
}

// Focus focuses the editor and returns the focus command.
func (m *Model) Focus() tea.Cmd { return m.ta.Focus() }

// Blur removes focus.
func (m *Model) Blur() { m.ta.Blur() }

// Value returns the composed message.
func (m Model) Value() string { return m.ta.Value() }

// SetValue replaces the editor contents.
func (m *Model) SetValue(v string) { m.ta.SetValue(v) }

// LineCount returns the number of lines currently in the editor.
func (m *Model) LineCount() int { return m.ta.LineCount() }

// ScrollYOffset returns the first visible line of the editor.
func (m Model) ScrollYOffset() int { return m.ta.ScrollYOffset() }

// Reset clears the editor.
func (m *Model) Reset() { m.ta.Reset() }

// Update processes messages for the editor.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.ta, cmd = m.ta.Update(msg)
	return m, cmd
}

// View renders the editor.
func (m Model) View() string { return m.ta.View() }
