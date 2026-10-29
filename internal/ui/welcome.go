package ui

import (
	"image/color"
	"math"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/tone"
	"github.com/charmbracelet/x/ansi"
)

// The welcome screen is a thin twig that draws itself across the empty pane,
// sprouts leaves, and then sways gently while it waits for something to show.

const (
	welcomeFrameInterval = 90 * time.Millisecond
	welcomeBranchWidth   = 56
	welcomeBranchRows    = 5
	welcomeGrowFrames    = 26
)

// welcomeTickMsg advances the welcome animation by one frame.
type welcomeTickMsg struct{}

func welcomeTick() tea.Cmd {
	return tea.Tick(welcomeFrameInterval, func(time.Time) tea.Msg {
		return welcomeTickMsg{}
	})
}

// branchCurve returns the row of the twig at column x for the given phase.
func branchCurve(x, width int, phase float64) int {
	t := float64(x) / float64(width)
	y := float64(welcomeBranchRows/2) + 0.8*math.Sin(t*math.Pi*2.2+phase)
	return max(0, min(welcomeBranchRows-1, int(math.Round(y))))
}

// welcomeArt draws the twig for the given frame: it grows from left to right
// first, then sways in place with twinkling leaves.
func welcomeArt(width, frame int) []string {
	grid := make([][]rune, welcomeBranchRows)
	for i := range grid {
		grid[i] = make([]rune, width)
		for j := range grid[i] {
			grid[i][j] = ' '
		}
	}

	grown := width
	if frame < welcomeGrowFrames {
		grown = min(width, 12+frame*2)
	}

	phase := 0.7
	if frame > welcomeGrowFrames {
		phase += float64(frame-welcomeGrowFrames) * 0.12
	}

	// The twig itself: a shallow sine wave drawn with box-drawing segments.
	// branchRow remembers which row each column drew on, so buds can sit
	// exactly on the line.
	branchRow := make([]int, width)
	prevY := branchCurve(0, width, phase)
	for x := 0; x < grown; x++ {
		y := branchCurve(x, width, phase)
		switch {
		case x == 0, y == prevY:
			grid[y][x] = '─'
			branchRow[x] = y
		case y < prevY:
			grid[prevY][x] = '╱'
			branchRow[x] = prevY
		default:
			grid[prevY][x] = '╲'
			branchRow[x] = prevY
		}
		prevY = y
	}

	// Buds sprout straight off the twig at regular intervals.
	for x := 6; x < grown; x += 8 {
		grid[branchRow[x]][x] = leafGlyph(frame, x)
	}

	// A bud at the growing tip draws the eye as the twig extends.
	if grown < width {
		tip := max(0, grown-1)
		grid[branchRow[tip]][tip] = leafGlyph(frame, grown)
	}

	palette := blendStops(width, []color.Color{tone.Orchid, tone.Mauve, tone.Blush})
	rows := make([]string, 0, welcomeBranchRows)
	for _, row := range grid {
		rows = append(rows, styleBranchRow(row, palette))
	}
	return rows
}

// leafGlyph alternates between a full blossom and a bud on slow frames.
func leafGlyph(frame, x int) rune {
	if (frame/3+x)%2 == 0 {
		return '✦'
	}
	return '✧'
}

// styleBranchRow renders one row of the twig, giving the branch a horizontal
// gradient and the leaves a warm accent.
func styleBranchRow(row []rune, palette []color.Color) string {
	var b strings.Builder
	for x, r := range row {
		if r == ' ' {
			b.WriteByte(' ')
			continue
		}
		st := lipgloss.NewStyle()
		switch r {
		case '✦', '✧':
			st = st.Foreground(tone.Butter)
			if (x/9)%2 == 1 {
				st = st.Foreground(tone.Julep)
			}
		default:
			if x < len(palette) && palette[x] != nil {
				st = st.Foreground(palette[x])
			}
		}
		b.WriteString(st.Render(string(r)))
	}
	return b.String()
}

// welcomeView renders the splash screen inside the body area.
func (m model) welcomeView() string {
	st := m.styles

	branchW := max(16, min(welcomeBranchWidth, max(1, m.width)-6))
	art := welcomeArt(branchW, m.welcomeFrame)
	status := "no changes"
	if m.branch != "" {
		status = "working tree clean · " + m.branch
	}

	hints := st.GrayFg("1") + st.Subtle().Render(" files   ") +
		st.GrayFg("2") + st.Subtle().Render(" commits   ") +
		st.GrayFg("t") + st.Subtle().Render(" tree   ") +
		st.GrayFg("?") + st.Subtle().Render(" help   ") +
		st.GrayFg("q") + st.Subtle().Render(" quit")

	helpH := 0
	if m.help {
		helpH = m.helpHeight()
	}
	bodyH := max(1, m.height-2-helpH)
	w := max(1, m.width)

	block := strings.Join([]string{
		strings.Join(art, "\n"),
		"",
		gradientText("  twig  ", tone.Charple, tone.Mauve, tone.Dolly),
		st.Subtle().Render("git diffs, without leaving the terminal"),
		"",
		st.Subtle().Render(status),
		"",
		hints,
	}, "\n")

	// Keep every line inside the body: Place centers, it does not clip.
	lines := strings.Split(block, "\n")
	if len(lines) > bodyH {
		lines = lines[:bodyH]
	}
	for i, line := range lines {
		if ansi.StringWidth(line) > w {
			lines[i] = ansi.Truncate(line, w, "…")
		}
	}
	block = strings.Join(lines, "\n")

	// Center every line on the body width so the twig, wordmark and hints
	// share one axis.
	block = lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render(block)

	return lipgloss.Place(w, bodyH, lipgloss.Center, lipgloss.Center, block)
}

// showWelcome reports whether the splash should replace the panes: the active
// tab has nothing to browse. The main pane may still hold a diff from the
// other tab, which is exactly the stale content the splash replaces.
func (m model) showWelcome() bool {
	if m.state != stateShowList || m.err != nil {
		return false
	}
	return m.loaded && m.files.Total() == 0
}
