package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/ui/styles"
)

// helpEntry is a keystroke and its action, rendered glow-style.
type helpEntry struct{ key, val string }

// helpColumn is a group of entries rendered into one column.
type helpColumn []helpEntry

func newHelpColumn(pairs ...string) helpColumn {
	var h helpColumn
	for i := 0; i+1 < len(pairs); i += 2 {
		h = append(h, helpEntry{key: pairs[i], val: pairs[i+1]})
	}
	return h
}

// render returns styled rows from keys and values, like glow's stashhelp.
func (h helpColumn) render(st styles.Styles, height int) []string {
	keyWidth, valWidth := h.maxWidths()

	rows := make([]string, 0, height)
	for i := 0; i < height; i++ {
		var b strings.Builder
		var k, v string
		if i < len(h) {
			k, v = h[i].key, h[i].val
		}
		b.WriteString(st.GrayFg(k))
		b.WriteString(strings.Repeat(" ", keyWidth-ansi.StringWidth(k)))
		b.WriteString("  ")
		b.WriteString(st.MidGrayFg(v))
		b.WriteString(strings.Repeat(" ", valWidth-ansi.StringWidth(v)))
		rows = append(rows, b.String())
	}
	return rows
}

func (h helpColumn) maxWidths() (keyWidth, valWidth int) {
	for _, e := range h {
		if w := ansi.StringWidth(e.key); w > keyWidth {
			keyWidth = w
		}
		if w := ansi.StringWidth(e.val); w > valWidth {
			valWidth = w
		}
	}
	return keyWidth, valWidth
}

// columnsToString joins columns side by side with a gutter, like glow: a
// two-space left gutter and four spaces between columns.
func columnsToString(cols ...[]string) string {
	var (
		out  []string
		maxH int
	)

	// Each column is padded to its own width, so short columns (or rows past
	// the end of a column) never shift the columns to their right.
	widths := make([]int, len(cols))
	for j, col := range cols {
		if len(col) > maxH {
			maxH = len(col)
		}
		for _, row := range col {
			widths[j] = max(widths[j], lipgloss.Width(row))
		}
	}

	for i := 0; i < maxH; i++ {
		var row strings.Builder
		row.WriteString("  ") // gutter, like glow
		for j, col := range cols {
			if j > 0 {
				row.WriteString("    ")
			}
			text := ""
			if i < len(col) {
				text = col[i]
			}
			row.WriteString(text)
			row.WriteString(strings.Repeat(" ", max(0, widths[j]-lipgloss.Width(text))))
		}
		out = append(out, row.String())
	}

	return strings.Join(out, "\n")
}

// helpView renders the help panel exactly like glow's: plain gray text on the
// app background, no colored bars behind the keys.
func helpView(st styles.Styles, width int, cols ...helpColumn) (string, int) {
	var rendered [][]string
	for _, c := range cols {
		rendered = append(rendered, c.render(st, len(c)))
	}

	s := columnsToString(rendered...)

	if width > 0 {
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			n := max(0, width-ansi.StringWidth(l))
			lines[i] = l + strings.Repeat(" ", n)
		}
		s = strings.Join(lines, "\n")
	}

	rows := 0
	for _, c := range cols {
		rows = max(rows, len(c))
	}
	return s, rows
}
