package ui

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/tone"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/ui/components/composer"
	"samouly.fun/twig/internal/ui/components/filelist"
	"samouly.fun/twig/internal/ui/components/statusbar"
)

// View implements tea.Model.
func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "twig"
	// The whole app paints on CharmTone Pepper.
	v.BackgroundColor = tone.Pepper
	return v
}

func (m model) render() string {
	if m.width < 30 || m.height < 6 {
		return m.tooSmallView()
	}

	switch m.state {
	case stateFatal:
		return m.errorView()
	case stateCompose:
		return m.composeModal()
	default:
		return m.pageView()
	}
}

func (m model) tooSmallView() string {
	msg := m.styles.Subtle().Render(
		fmt.Sprintf("terminal too small (%d x %d)", m.width, m.height),
	)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
}

// errorView is a port of glow's errorView.
func (m model) errorView() string {
	s := fmt.Sprintf(
		"%s\n\n%v\n\n%s",
		m.styles.ErrorTitle().Render("ERROR"),
		m.err,
		m.styles.Subtle().Render("press any key to exit"),
	)
	return "\n" + indent(s, 3)
}

// pageView renders a tab page: header bar, panes, footer bar, and — like
// glow — the help view below the footer bar. When there is nothing to browse
// an animated welcome screen takes the place of the panes.
func (m model) pageView() string {
	body := m.bodyView()
	if m.showWelcome() {
		body = m.welcomeView()
	}

	var b strings.Builder
	b.WriteString(m.headerView())
	b.WriteByte('\n')
	b.WriteString(body)
	b.WriteByte('\n')
	b.WriteString(m.footerView())
	b.WriteByte('\n')
	if m.help {
		help, _ := m.helpView()
		b.WriteString(help)
	}
	return b.String()
}

// headerView renders the persistent top bar: gradient logo, project title,
// branch fill and the Files/Commits segments on the right.
func (m model) headerView() string {
	st := m.styles

	tabs := []statusbar.Tab{
		{Label: "1 Files", Active: m.files.Tab() == filelist.TabFiles},
		{Label: "2 Commits", Active: m.files.Tab() == filelist.TabCommits},
	}

	var right string
	if m.d != nil {
		right = st.GreenFg("+"+strconv.Itoa(m.d.Additions)) + " " +
			st.RedFg("-"+strconv.Itoa(m.d.Deletions))
	}

	logoText := " Twig "
	if name := m.project; name != "" {
		// The gradient owns the logo, the divider dot and the project name
		// so they read as one continuous segment.
		logoText = " Twig · " + name + " "
	}

	return statusbar.HeaderView(st, m.width, statusbar.HeaderInfo{
		Logo:  gradientText(logoText, tone.Charple, tone.Mauve, tone.Dolly),
		Tabs:  tabs,
		Fill:  m.branch + " · " + m.src.Describe(),
		Right: right,
	})
}

// gradientText renders text with a horizontal gradient foreground across the
// given CharmTone stops. It paints on the app background, so the letters keep
// full contrast instead of sitting on a saturated block.
func gradientText(text string, stops ...color.Color) string {
	runes := []rune(text)
	if len(runes) == 0 {
		return ""
	}
	colors := blendStops(len(runes), stops)

	var b strings.Builder
	for i, r := range runes {
		st := lipgloss.NewStyle().Bold(true).Foreground(tone.Mauve)
		if i < len(colors) && colors[i] != nil {
			st = st.Foreground(colors[i])
		}
		b.WriteString(st.Render(string(r)))
	}
	return b.String()
}

// blendStops spreads a set of colors evenly over n steps, interpolating
// between consecutive stops.
func blendStops(n int, stops []color.Color) []color.Color {
	if n <= 0 || len(stops) == 0 {
		return nil
	}
	if n == 1 || len(stops) == 1 {
		out := make([]color.Color, n)
		for i := range out {
			out[i] = stops[0]
		}
		return out
	}

	out := make([]color.Color, 0, n)
	segments := len(stops) - 1
	per := float64(n-1) / float64(segments)
	for i := 0; i < n; i++ {
		seg := min(int(float64(i)/per), segments-1)
		frac := (float64(i) - float64(seg)*per) / per
		out = append(out, blendColor(stops[seg], stops[seg+1], frac))
	}
	return out
}

// blendColor interpolates two colors in sRGB space.
func blendColor(from, to color.Color, t float64) color.Color {
	t = max(0, min(1, t))
	fr, fg, fb, _ := from.RGBA()
	tr, tg, tb, _ := to.RGBA()
	lerp := func(a, b uint32) uint8 {
		return uint8((float64(a)*(1-t) + float64(b)*t) / 257)
	}
	return color.NRGBA{R: lerp(fr, tr), G: lerp(fg, tg), B: lerp(fb, tb), A: 0xff}
}

// bodyView renders the pane split for the active tab. Either sidebar can be
// hidden; the main pane then takes the freed space.
func (m model) bodyView() string {
	var panes []string
	if m.showLeft {
		panes = append(panes, m.pane(m.files.View(), m.focus == focusSidebar, m.sideRect))
	}
	panes = append(panes, m.mainPane())
	if m.files.Tab() == filelist.TabCommits && m.showRight {
		panes = append(panes, m.pane(m.files.CommitFilesView(), m.focus == focusCommitFiles, m.commitFilesRct))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, panes...)
}

// mainPane renders the main content pane: the diff of the current selection,
// with the search bar line pinned to its bottom while active.
func (m model) mainPane() string {
	var content string
	if !m.loaded || (m.view.Empty() && (m.loading() || m.allLoading)) {
		content = "\n" + m.spinner.View() + " " +
			m.styles.Subtle().Render("loading…") + "\n"
	} else {
		content = m.view.View()
	}

	if m.search.Active() {
		matchMsg := ""
		if m.view.SearchTerm() != "" {
			matchMsg = fmt.Sprintf("%d matches", m.view.MatchCount())
		}
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			content,
			m.search.View(m.mainRect.Dx()-2, matchMsg),
		)
	}
	return m.pane(content, m.focus == focusMain, m.mainRect)
}

func (m model) loading() bool {
	return m.view.Empty() && !m.loaded
}

// pane wraps content in a soft-serve-style bordered pane whose border color
// reflects focus. Content is clipped to the pane body so the chrome (footer
// bar, help view) can never be pushed off-screen by overflowing content.
func (m model) pane(content string, focused bool, rect uv.Rectangle) string {
	st := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder())
	if focused {
		st = st.BorderForeground(m.styles.BorderFocused)
	} else {
		st = st.BorderForeground(m.styles.BorderBlurred)
	}
	return st.
		Width(max(1, rect.Dx())).
		Height(max(1, rect.Dy())).
		MaxWidth(max(1, rect.Dx())).
		MaxHeight(max(1, rect.Dy())).
		Render(content)
}

// footerView renders the persistent bottom bar with per-state segments.
func (m model) footerView() string {
	st := m.styles

	info := statusbar.Info{
		Message: m.message,
	}

	switch m.files.Tab() {
	case filelist.TabCommits:
		info.State = "COMMITS"
		info.KeyStyle = st.FooterKeyCommits()
		info.Value = plural(m.files.Total(), "commit")
		if c, ok := m.files.SelectedCommit(); ok {
			info.Value = c.Short + " " + c.Subject
		}
		info.Info = position(m.files.SelectedIndex()+1, m.files.Total()) + m.viewModeLabel()
	default:
		info.State = "FILES"
		info.KeyStyle = st.FooterKeyFiles()
		var adds, dels int
		if m.d != nil {
			adds, dels = m.d.Additions, m.d.Deletions
		}
		info.Value = plural(m.files.Total(), "file") + "  +" +
			strconv.Itoa(adds) + " -" + strconv.Itoa(dels)
		if f, _ := m.files.SelectedFile(); f.Path != "" {
			info.Value = f.Letter + " " + f.Path
		}
		info.Info = position(m.files.SelectedOrdinal(), m.files.Total()) + m.viewModeLabel()
	}

	return m.status.FooterView(info)
}

// viewModeLabel is appended to the footer info pill so the active diff
// layout is always visible.
func (m model) viewModeLabel() string {
	if m.view.Split() {
		return " split"
	}
	return ""
}

// composeModal renders the commit dialog over the current page. The page is
// dimmed cell by cell (colors are scaled down, not replaced) and the dialog is
// drawn on top at the center, like a web modal over a scrim.
func (m model) composeModal() string {
	base := m.pageView()
	modal, inputRect := m.composeDialogParts()
	modalW, modalH := lipgloss.Width(modal), lipgloss.Height(modal)

	x := max(0, (m.width-modalW)/2)
	y := max(0, (m.height-modalH)/2)

	canvas := lipgloss.NewCanvas(max(1, m.width), max(1, m.height))
	canvas.Compose(lipgloss.NewLayer(base))
	dimCanvas(canvas, 0.45)
	lipgloss.NewLayer(modal).Draw(canvas, uv.Rect(x, y, modalW, modalH))

	// The textarea's own ANSI resets can leave cells transparent; repaint the
	// editor rectangle so it is a full, unbroken panel whether or not the
	// user has typed anything.
	paintCanvasRect(canvas, uv.Rect(
		x+inputRect.Min.X, y+inputRect.Min.Y, inputRect.Dx(), inputRect.Dy(),
	), m.styles.ModalInput().GetBackground())

	return fitBlock(canvas.Render(), max(1, m.width), max(1, m.height))
}

// paintCanvasRect fills transparent cells inside a rectangle with a
// background color, leaving styled cells untouched.
func paintCanvasRect(c *lipgloss.Canvas, r uv.Rectangle, bg color.Color) {
	if bg == nil {
		return
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			cell := c.CellAt(x, y)
			if cell == nil || cell.Style.Bg != nil {
				continue
			}
			filled := cell.Clone()
			filled.Style.Bg = bg
			c.SetCell(x, y, filled)
		}
	}
}

// composeDialog renders the dialog on its own.
func (m model) composeDialog() string {
	dialog, _ := m.composeDialogParts()
	return dialog
}

// composeDialogParts builds the dialog and returns the rectangle of its
// editor panel, relative to the dialog's top-left corner.
func (m model) composeDialogParts() (string, uv.Rectangle) {
	innerW := max(20, min(66, m.width-4))
	boxW := innerW + 4 // rounded border plus one cell of padding each side

	dialog := m.styles.ModalPanel() // Pepper, the app background
	input := m.styles.ModalInput()  // Char, the lifted editor
	inputBG := input.GetBackground()
	border := m.styles.ModalBorder() // Hazy frame on Pepper

	staged := m.files.StagedFiles()
	title := "Commit staged changes"
	switch len(staged) {
	case 1:
		title = "Commit 1 staged file"
	default:
		if len(staged) > 1 {
			title = "Commit " + itoa(len(staged)) + " staged files"
		}
	}

	editor := m.compose
	editor.SetBackground(inputBG)
	// The cursor line shares the panel color: the editor must read as one
	// solid block, with only the cursor itself marking the caret.
	editor.SetCursorLineColor(inputBG)
	editor.SetSelectionColors(tone.Hazy, tone.Pepper)

	// The editor shows three lines; a right scrollbar appears once the
	// message outgrows them.
	const editorH = 3
	total := editor.LineCount()
	barW := 0
	if total > editorH {
		barW = 1
	}
	editor.SetEditorSize(innerW-barW, editorH)

	row := func(text string) string {
		text = ansi.Truncate(text, innerW, "…")
		pad := max(0, innerW-ansi.StringWidth(text))
		return border.Render("│") + " " + dialog.Render(text+strings.Repeat(" ", pad)) + " " + border.Render("│")
	}

	// Rows: staged files, a blank, then the editor panel.
	body := make([]string, 0, len(staged)+2)
	for i, f := range staged {
		if i == modalFileRows {
			body = append(body, m.styles.ModalHint().Render(
				fmt.Sprintf("… and %d more", len(staged)-modalFileRows),
			))
			break
		}
		body = append(body, m.fileRow(f))
	}
	body = append(body, "")

	lines := make([]string, 0, len(body)+editorH+2)
	lines = append(lines, m.dialogTop(title, boxW))
	for _, r := range body {
		lines = append(lines, row(r))
	}
	// The editor block starts right after the top border and the body rows.
	inputRect := uv.Rect(2, 1+len(body), innerW, editorH)
	lines = append(lines, m.dialogInputRows(editor, innerW, editorH, barW, input)...)
	lines = append(lines, m.dialogBottom(boxW))

	return strings.Join(lines, "\n"), inputRect
}

// dialogTop draws the top edge with the title embedded in it:
// ╭─ Title ──────────────────╮
func (m model) dialogTop(title string, boxW int) string {
	border := m.styles.ModalBorder()
	label := ansi.Truncate(m.styles.ModalTitle().Render(title), max(1, boxW-4), "…")
	dashes := max(0, boxW-3-lipgloss.Width(label))
	return border.Render("╭─") + label + border.Render(strings.Repeat("─", dashes)+"╮")
}

// dialogBottom draws the bottom edge with the key hints embedded in it:
// ╰─ ctrl+s commit   esc cancel ──────────╯
func (m model) dialogBottom(boxW int) string {
	border := m.styles.ModalBorder()
	hints := m.styles.ModalKeycap().Render("ctrl+s") + " " +
		m.styles.ModalHint().Render("commit") +
		m.styles.ModalHint().Render("   ") +
		m.styles.ModalKeycap().Render("esc") + " " +
		m.styles.ModalHint().Render("cancel") + " "
	hints = ansi.Truncate(hints, max(1, boxW-4), "…")
	dashes := max(0, boxW-3-lipgloss.Width(hints))
	return border.Render("╰─") + hints + border.Render(strings.Repeat("─", dashes)+"╯")
}

// dialogInputRows renders the editor block: a full BBQ panel with an optional
// scrollbar on its right edge.
func (m model) dialogInputRows(editor composer.Model, innerW, height, barW int, input lipgloss.Style) []string {
	border := m.styles.ModalBorder()
	bg := input.GetBackground()

	editorW := innerW - barW
	editor.SetEditorSize(editorW, height)

	textLines := strings.Split(editor.View(), "\n")
	for len(textLines) < height {
		textLines = append(textLines, "")
	}

	thumbStart, thumbLen := 0, 0
	total := editor.LineCount()
	if barW > 0 && total > height {
		thumbLen = max(1, height*height/total)
		maxOff := max(1, total-height)
		off := min(max(0, editor.ScrollYOffset()), maxOff)
		thumbStart = off * (height - thumbLen) / maxOff
	}

	track := lipgloss.NewStyle().Background(bg).Foreground(tone.Iron).Render("│")
	thumb := lipgloss.NewStyle().Background(bg).Foreground(tone.Hazy).Bold(true).Render("┃")

	rows := make([]string, 0, height)
	for i := 0; i < height; i++ {
		text := ansi.Truncate(textLines[i], editorW, "…")
		pad := max(0, editorW-ansi.StringWidth(text))
		line := input.Render(text + strings.Repeat(" ", pad))

		if barW > 0 {
			cell := track
			if i >= thumbStart && i < thumbStart+thumbLen {
				cell = thumb
			}
			line += cell
		}
		rows = append(rows, border.Render("│")+" "+line+" "+border.Render("│"))
	}
	return rows
}

// modalFileRows is how many staged files the dialog lists before folding the
// rest into an "and N more" line.
const modalFileRows = 4

// fileRow renders one staged file inside the dialog: status letter and path.
func (m model) fileRow(f git.FileStatus) string {
	letter := f.Letter
	if f.Untracked {
		letter = "?"
	}

	mark := m.styles.MidGrayFg(letter)
	switch letter {
	case "A":
		mark = m.styles.GreenFg(letter)
	case "D":
		mark = m.styles.RedFg(letter)
	}
	return mark + " " + m.styles.MidGrayFg(f.Path)
}

// dimCanvas scales every cell's colors down so the page reads as the backdrop
// of a dialog while keeping its structure and hues. Cells with no explicit
// foreground (plain text such as the header fill and the line-number gutter)
// are dimmed too, otherwise they would stay at full brightness.
func dimCanvas(c *lipgloss.Canvas, factor float64) {
	b := c.Bounds()
	scrim := dimColor(tone.Pepper, factor)
	text := dimColor(tone.Steep, factor)

	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			cell := c.CellAt(x, y)
			if cell == nil {
				continue
			}
			dimmed := cell.Clone()
			if dimmed.Style.Fg == nil {
				dimmed.Style.Fg = text
			} else {
				dimmed.Style.Fg = dimColor(dimmed.Style.Fg, factor)
			}
			dimmed.Style.UnderlineColor = dimColor(dimmed.Style.UnderlineColor, factor)
			if dimmed.Style.Bg == nil {
				dimmed.Style.Bg = scrim
			} else {
				dimmed.Style.Bg = dimColor(dimmed.Style.Bg, factor)
			}
			c.SetCell(x, y, dimmed)
		}
	}
}

// dimColor multiplies a color's channels, leaving nil colors alone.
func dimColor(c color.Color, factor float64) color.Color {
	if c == nil {
		return nil
	}
	r, g, b, a := c.RGBA()
	scale := func(v uint32) uint8 {
		return uint8(min(255.0, float64(v>>8)*factor))
	}
	return color.NRGBA{R: scale(r), G: scale(g), B: scale(b), A: uint8(a >> 8)}
}

// fitBlock pads a rendered block to the full terminal, so the composited view
// covers the whole screen.
func fitBlock(block string, w, h int) string {
	lines := strings.Split(block, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for i, line := range lines {
		if pad := w - ansi.StringWidth(line); pad > 0 {
			lines[i] = line + strings.Repeat(" ", pad)
		}
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return strings.Join(lines, "\n")
}

// helpView renders the help columns for the active tab.
func (m model) helpView() (string, int) {
	return helpView(m.styles, m.width, m.helpColumns()...)
}

// helpHeight is the number of rows the help view occupies for the active
// tab, so the panes shrink by exactly that amount instead of being clipped.
func (m model) helpHeight() int {
	rows := 0
	for _, c := range m.helpColumns() {
		rows = max(rows, len(c))
	}
	return rows
}

// helpColumns returns the key reference columns for the active tab.
func (m model) helpColumns() []helpColumn {
	if m.files.Tab() == filelist.TabCommits {
		c1 := newHelpColumn(
			"k/↑", "up",
			"j/↓", "down",
			"enter", "open commit",
			"tab", "next pane",
			"S-tab", "prev pane",
			"esc", "back",
		)
		c2 := newHelpColumn(
			"g/home", "top",
			"G/end", "bottom",
			"[", "prev hunk",
			"]", "next hunk",
			"y", "yank patch",
			"Y", "yank hunk",
		)
		c3 := newHelpColumn(
			"/", "search",
			"n", "next match",
			"N", "prev match",
			"v", "split view",
			"w", "wrap",
			"Z", "ignore ws",
			"c", "context",
		)
		c4 := newHelpColumn(
			"C", "commit",
			"P", "push",
			"r", "refresh",
			"1/2", "tabs",
			"H/L", "sidebars",
			"?", "hide help",
			"q", "quit",
		)
		return []helpColumn{c1, c2, c3, c4}
	}

	c1 := newHelpColumn(
		"k/↑", "up",
		"j/↓", "down",
		"enter", "open diff",
		"tab", "next pane",
		"S-tab", "prev pane",
		"esc", "back",
	)
	c2 := newHelpColumn(
		"/", "find",
		"s", "stage",
		"u", "unstage",
		"C", "commit",
		"P", "push",
		"r", "refresh",
	)
	c3 := newHelpColumn(
		"J", "next file",
		"K", "prev file",
		"t", "tree view",
		"a", "all changes",
		"v", "split view",
		"1/2", "tabs",
		"H/L", "sidebars",
		"?", "hide help",
		"q", "quit",
	)
	return []helpColumn{c1, c2, c3}
}

// indent pads every non-empty line, like glow's indent helper.
func indent(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	pad := strings.Repeat(" ", n)
	var b strings.Builder
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		if strings.TrimSpace(line) != "" {
			b.WriteString(pad)
		}
		b.WriteString(line)
	}
	return b.String()
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

func position(i, total int) string {
	if total == 0 {
		return "0/0"
	}
	return strconv.Itoa(i) + "/" + strconv.Itoa(total)
}
