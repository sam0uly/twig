package statusbar

import (
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/tone"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/ui/styles"
)

// NoticeDuration is how long a notice's body scrambles before it settles.
const NoticeDuration = 350 * time.Millisecond

// noticeFrame is the step between scramble frames.
const noticeFrame = 25 * time.Millisecond

// scrambleGlyphs is the character soup a scrambling body shows: plain and
// accented letters, Greek, digits and a wide spread of symbols. Every rune is a
// single cell wide, so the bar never shifts. The symbols are listed three times
// so they land as often as letters and digits, which is what makes the noise
// look like noise.
var scrambleGlyphs = []rune(
	"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz" +
		"ßñøåçþðæœλπΣΔΩµ" +
		"0123456789" +
		scrambleSymbols + scrambleSymbols + scrambleSymbols,
)

// scrambleSymbols are the punctuation and signs the soup is built from.
const scrambleSymbols = "€£¥¢§¶¤©®™‰°±†‡•‣¿¡*@$#!%&+=/<>?^~" +
	"∞≈≠≤≥∴∵∈∉∩∪⊂⊃∅∧∨¬∀∃∂∫∑∏√∝∠⊥∥" +
	"◊●○■□▲▼◆▪▫►◄⊕⊖⊗⊘" +
	"←↑→↓↔⇒⇐⇄⇅↺↻" +
	"♠♣♥♦♪♫☀☂☃☄❄❅❆" +
	"★☆✦✧✳✴❇" +
	"✓✔✗✘" +
	"⌘⌥⌃⇧⌫⌦⏎⇥⎋⚙⛏☘➔➜➤"

// Notice is the transient footer message: a status word and a body that
// scrambles in, settles, and scrambles out again when it is done.
type Notice struct {
	Kind  styles.NoticeKind
	Body  string
	Since time.Time
	Now   time.Time
	// Left is when the notice started scrambling out; zero means it is still
	// arriving or resting.
	Left time.Time
}

// progress is how far the scramble has come, from 0 (all noise) to 1 (settled),
// and whether the notice is on its way out.
func (n Notice) progress() (float64, bool) {
	if !n.Left.IsZero() {
		elapsed := n.Now.Sub(n.Left)
		if elapsed >= NoticeDuration {
			return 0, true
		}
		return 1 - float64(max(0, elapsed))/float64(NoticeDuration), true
	}
	elapsed := n.Now.Sub(n.Since)
	if elapsed >= NoticeDuration {
		return 1, false
	}
	return float64(max(0, elapsed)) / float64(NoticeDuration), false
}

// noticeSegment renders a notice across exactly width cells: the word in a
// pill of its own colour, a cell of gap, then the body on the family's wash,
// scrambled and shaded by a gradient until it settles.
func (m Model) noticeSegment(n Notice, width int) string {
	if width <= 0 {
		return ""
	}
	label, pill, textColor, wash := m.styles.Notice(n.Kind)
	// The word is a pill padded a cell on either side; the body follows a cell
	// later so the two never touch.
	word := " " + label + " "
	if lipgloss.Width(word) > width {
		word = ansi.Truncate(word, width, "…")
	}
	wordStyle := lipgloss.NewStyle().Bold(true).Foreground(tone.Salt).Background(pill)
	bodyStyle := lipgloss.NewStyle().Foreground(textColor).Background(wash)

	lead := min(1, max(0, width-lipgloss.Width(word)))
	room := max(0, width-lipgloss.Width(word)-lead)
	body := []rune(ansi.Truncate(n.Body, room, "…"))
	// The padding is there to fill the bar, not to be revealed: the scramble
	// resolves the text's own runes, left to right.
	text := len(body)

	progress, leaving := n.progress()
	elapsed := n.Now.Sub(n.Since)
	frame := int64(elapsed / noticeFrame)
	// A leaving notice dissolves in its own order, so the way out never mirrors
	// the way in.
	seed := n.Since.UnixNano()
	if leaving {
		seed = n.Left.UnixNano()
	}
	ramp := lipgloss.Blend1D(max(1, text), pill, textColor)

	var b strings.Builder
	b.WriteString(wordStyle.Render(word))
	if lead > 0 {
		b.WriteString(bodyStyle.Render(" "))
	}
	for i := 0; i < room; i++ {
		c := ' '
		noise := false
		if i < text {
			c = body[i]
			// Each position settles at its own moment, so the body resolves in
			// a scattered order instead of sweeping across.
			noise = c != ' ' && progress < settleAt(seed, i)
			if noise {
				c = scrambleRune(frame, i)
			}
		}
		style := bodyStyle
		if noise {
			// The noise shimmers through the family's ramp while the settled
			// text sits beside it in the body tone.
			style = lipgloss.NewStyle().
				Foreground(ramp[(i+int(frame))%len(ramp)]).
				Background(wash)
		}
		b.WriteString(style.Render(string(c)))
	}
	return b.String()
}

// settleAt is the moment position i stops scrambling, as a fraction of the
// animation in [0,1). It is a hash of the notice and the position, so each
// notice resolves in its own order.
func settleAt(seed int64, i int) float64 {
	h := uint64(seed) ^ (uint64(i)+1)*0x9e3779b97f4a7c15
	h ^= h >> 30
	h *= 0xbf58476d1ce4e5b9
	h ^= h >> 27
	h *= 0x94d049bb133111eb
	h ^= h >> 31
	return float64(h>>11) / float64(uint64(1)<<53)
}

// scrambleRune is the glyph shown at position i on a given frame. It is a hash
// rather than a random source, so a frame draws the same however often it is
// rendered.
func scrambleRune(frame int64, i int) rune {
	h := uint64(frame)*2654435761 + uint64(i)*40503 + 0x9e3779b97f4a7c15
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 29
	return scrambleGlyphs[h%uint64(len(scrambleGlyphs))]
}
