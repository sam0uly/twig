package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/tone"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/ui/components/filelist"
)

// The welcome screen is a mock git log graph: commits scroll up a main line,
// a branch forks off it and merges back, and the newest commit pulses. Every
// row is generated from its absolute stream index, so any frame is a pure
// function of its number and nothing has to be kept between frames.

const (
	welcomeFrameInterval = 70 * time.Millisecond
	welcomeRows          = 9
	// welcomeRevealFrames is how many frames the graph takes to print its
	// first screen, one row per frame.
	welcomeRevealFrames = welcomeRows
	welcomeScrollFrames = 3 // frames per row once the graph is up
	// welcomeStreamCycle is the row period of the mock graph: a branch forks,
	// carries its own commits and merges back inside one screen.
	welcomeStreamCycle = 9
	welcomeHashWidth   = 7
	welcomeGraphWidth  = 3
)

// welcomeSubjects are the mock commit subjects, one per stream row.
var welcomeSubjects = []string{
	"anchor first, diff the gaps",
	"pair changed lines by token similarity",
	"keep runs instead of rows",
	"page a huge diff in blocks",
	"cache blobs by object id",
	"slide hunks to a readable edge",
	"intern lines behind a 64-bit hash",
	"bound the walk by bytes, not entries",
	"read the repository through git",
	"highlight only the word that changed",
	"stream a commit's files lazily",
	"name the band a missing blob leaves",
}

// welcomeTickMsg advances the welcome animation by one frame.
type welcomeTickMsg struct{}

func welcomeTick() tea.Cmd {
	return tea.Tick(welcomeFrameInterval, func(time.Time) tea.Msg {
		return welcomeTickMsg{}
	})
}

// fakeHash returns a stable seven-character object id for a stream row.
func fakeHash(row int) string {
	h := uint32(row)*0x9e3779b1 + 0x7f4a7c15
	h ^= h >> 15
	h *= 0x85ebca6b
	h ^= h >> 13
	return fmt.Sprintf("%0*x", welcomeHashWidth, h>>4&0xfffffff)
}

// streamSubject picks the mock subject for a stream row.
func streamSubject(row int) string {
	return welcomeSubjects[(row*5+row/3)%len(welcomeSubjects)]
}

// welcomeArt renders the mock log for one frame.
func welcomeArt(width, frame int) []string {
	revealed := min(welcomeRows, 1+frame)
	base := 0
	if frame > welcomeRevealFrames {
		base = (frame - welcomeRevealFrames) / welcomeScrollFrames
	}

	rows := make([]string, 0, welcomeRows)
	for i := 0; i < welcomeRows; i++ {
		if i >= revealed {
			// Pad the blank rows too: the block is centered by line width,
			// so a short row would drag everything left.
			rows = append(rows, strings.Repeat(" ", width))
			continue
		}
		newest := i == revealed-1
		row := welcomeGraphRow(base+i, width, newest, frame)
		if pad := width - lipgloss.Width(row); pad > 0 {
			row += strings.Repeat(" ", pad)
		}
		rows = append(rows, row)
	}
	return rows
}

// welcomeGraphRow draws one row of the mock graph: the lane glyphs, a hash
// and a subject. The cycle forks a branch at one row, carries commits on it
// for a while, then merges it back into the main line.
func welcomeGraphRow(row, width int, newest bool, frame int) string {
	pos := row % welcomeStreamCycle
	mainCommit := pos == 0 || pos == 1 || pos == 4 || pos == 8
	branchCommit := pos == 3 || pos == 5
	fork := pos == 2
	merge := pos == 7
	branchLive := pos > 2 && pos < 7

	var (
		line      = lipgloss.NewStyle().Foreground(tone.Charple)
		branch    = lipgloss.NewStyle().Foreground(tone.Pickle)
		dot       = lipgloss.NewStyle().Foreground(tone.Blush).Bold(true)
		branchDot = lipgloss.NewStyle().Foreground(tone.Julep).Bold(true)
		hashStyle = lipgloss.NewStyle().Foreground(tone.Squid)
		subjStyle = lipgloss.NewStyle().Foreground(tone.Steep)
	)

	if newest {
		dot = lipgloss.NewStyle().Foreground(tone.Butter).Bold(true)
		branchDot = dot
		hashStyle = lipgloss.NewStyle().Foreground(tone.Smoke)
		subjStyle = lipgloss.NewStyle().Foreground(tone.Oyster).Bold(true)
		if (frame/6)%2 == 1 {
			dot = lipgloss.NewStyle().Foreground(tone.Dolly).Bold(true)
			branchDot = dot
		}
	}

	var g strings.Builder
	switch {
	case fork:
		g.WriteString(line.Render("├") + line.Render("─") + branch.Render("╮"))
	case merge:
		g.WriteString(line.Render("├") + line.Render("─") + branch.Render("╯"))
	default:
		if mainCommit {
			g.WriteString(dot.Render("●"))
		} else {
			g.WriteString(line.Render("│"))
		}
		g.WriteByte(' ')
		switch {
		case !branchLive:
			g.WriteByte(' ')
		case branchCommit:
			g.WriteString(branchDot.Render("●"))
		default:
			g.WriteString(branch.Render("│"))
		}
	}

	hash := fakeHash(row)
	subject := streamSubject(row)
	// The graph, one space, the hash, two spaces, then the subject.
	avail := max(1, width-welcomeGraphWidth-1-welcomeHashWidth-2)
	subject = ansi.Truncate(subject, avail, "…")

	return g.String() + " " + hashStyle.Render(hash) + "  " + subjStyle.Render(subject)
}

// welcomeView renders the splash screen inside the body area.
func (m model) welcomeView() string {
	st := m.styles

	artW := max(welcomeGraphWidth+welcomeHashWidth+8, min(58, max(1, m.width)-6))
	art := welcomeArt(artW, m.welcomeFrame)
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
		st.Subtle().Render("$ git log --graph --oneline --all --decorate"),
		"",
		strings.Join(art, "\n"),
		"",
		gradientPill(" twig ", logoStops...),
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
	// One block width for every line, so centering lines up the graph, the
	// caption and the wordmark instead of centering each on its own.
	widest := 0
	for _, line := range lines {
		widest = max(widest, ansi.StringWidth(line))
	}
	for i, line := range lines {
		if pad := widest - ansi.StringWidth(line); pad > 0 {
			lines[i] = line + strings.Repeat(" ", pad)
		}
	}
	block = strings.Join(lines, "\n")

	// Center every line on the body width so the graph, wordmark and hints
	// share one axis.
	block = lipgloss.NewStyle().Width(w).Align(lipgloss.Center).Render(block)

	return lipgloss.Place(w, bodyH, lipgloss.Center, lipgloss.Center, block)
}

// showWelcome reports whether the splash should replace the panes: the files
// tab has nothing to browse. The main pane may still hold a diff from the
// other tab, which is exactly the stale content the splash replaces. The
// commits tab never shows it, even with an empty log.
func (m model) showWelcome() bool {
	if m.state != stateShowList || m.err != nil {
		return false
	}
	if m.files.Tab() != filelist.TabFiles {
		return false
	}
	return m.loaded && m.files.Total() == 0
}
