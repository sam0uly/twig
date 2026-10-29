// Package statusbar renders twig's persistent chrome: a segmented header
// bar and a segmented footer bar in the style of soft-serve's status bar.
// The bars are always present; only their segments change per state.
package statusbar

import (
	"math"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"twig/internal/ui/styles"
)

// Tab is one segment in the header bar.
type Tab struct {
	Label  string
	Active bool
}

// HeaderInfo describes the header bar contents.
type HeaderInfo struct {
	// Logo is the pre-rendered logo segment (gradient background), including
	// the divider dot and project name when there is one.
	Logo  string
	Tabs  []Tab
	Fill  string
	Right string
}

// Info describes the footer bar contents.
type Info struct {
	// State is the footer key pill label, e.g. "FILES" or "DIFF".
	State string
	// KeyStyle picks the key pill colors for the state.
	KeyStyle lipgloss.Style
	// Value is the flexible middle segment.
	Value string
	// Info is the informational pill, e.g. scroll percent or position.
	Info string
	// Message replaces the value segment when set.
	Message string
}

// HeaderView renders the top bar: gradient logo, project title, flexible
// branch info and right-aligned tab segments.
func HeaderView(st styles.Styles, width int, info HeaderInfo) string {
	if width < 1 {
		return ""
	}

	left := info.Logo

	var tabs string
	for _, t := range info.Tabs {
		if t.Active {
			tabs += st.HeaderTabActive().Render(t.Label)
		} else {
			tabs += st.HeaderTab().Render(t.Label)
		}
	}

	right := info.Right
	if tabs != "" {
		// Tab segments live on the right; info.Right trails them.
		right = tabs + "  " + right
	}

	used := lipgloss.Width(left) + lipgloss.Width(right)
	fill := max(0, width-used)
	fillText := ""
	if fill > 0 {
		text := info.Fill
		if lipgloss.Width(text) > fill-2 {
			text = ansi.Truncate(text, max(0, fill-2), "…")
		}
		pad := max(0, fill-lipgloss.Width(text))
		fillText = strings.Repeat(" ", pad/2) + text + strings.Repeat(" ", pad-pad/2)
	}

	return lipgloss.NewStyle().Width(width).MaxWidth(width).
		Render(left + fillText + right)
}

// Model holds the footer bar state.
type Model struct {
	styles styles.Styles
	width  int
}

// New builds the status bar model.
func New(st styles.Styles) Model { return Model{styles: st, width: 80} }

// SetStyles updates the theme.
func (m *Model) SetStyles(st styles.Styles) { m.styles = st }

// SetWidth resizes the bar.
func (m *Model) SetWidth(width int) { m.width = width }

// FooterView renders the footer bar: state key pill, flexible value
// segment, info pill and help pill.
func (m Model) FooterView(info Info) string {
	st := m.styles
	w := m.width
	if w < 1 {
		return ""
	}

	key := info.KeyStyle.Render(" " + info.State + " ")
	help := st.FooterHelp().Render(" ? Help ")

	var infoSeg string
	if info.Info != "" {
		infoSeg = st.FooterInfo().Render(" " + info.Info + " ")
	}

	maxValue := w - lipgloss.Width(key) - lipgloss.Width(infoSeg) - lipgloss.Width(help)
	var value string
	if info.Message != "" {
		msg := ansi.Truncate(" "+info.Message+" ", max(0, maxValue), "…")
		pad := max(0, maxValue-lipgloss.Width(msg))
		value = st.FooterMessage().Render(msg + strings.Repeat(" ", pad))
	} else {
		v := ansi.Truncate(info.Value, max(0, maxValue-2), "…")
		pad := max(0, maxValue-lipgloss.Width(v)-2)
		value = st.FooterValue().Render(" " + v + strings.Repeat(" ", pad+2))
	}

	return lipgloss.NewStyle().Width(w).MaxWidth(w).
		Render(lipgloss.JoinHorizontal(lipgloss.Top, key, value, infoSeg, help))
}

// FormatPercent renders a 0..1 fraction as a padded percentage.
func FormatPercent(p float64) string {
	p = math.Max(0, math.Min(1, p))
	return strconv.Itoa(int(math.Round(p*100))) + "%"
}
