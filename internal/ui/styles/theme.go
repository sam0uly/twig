// Package styles holds the twig theme. Every color comes from the CharmTone
// palette (charm.land/tone) — nothing is hardcoded. The whole app paints on
// a Pepper background, with chrome in the spirit of soft-serve and diff
// washes in the spirit of Zed/VS Code.
package styles

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"charm.land/tone"
)

// Styles contains all styles used in the UI.
type Styles struct {
	dimNormalFg     func(...string) string
	brightGrayFg    func(...string) string
	dimBrightGrayFg func(...string) string
	grayFg          func(...string) string
	midGrayFg       func(...string) string
	darkGrayFg      lipgloss.Style
	greenFg         func(...string) string
	semiDimGreenFg  func(...string) string
	dimGreenFg      func(...string) string
	fuchsiaFg       func(...string) string
	dimFuchsiaFg    func(...string) string
	dullFuchsiaFg   func(...string) string
	redFg           func(...string) string

	errorTitleStyle lipgloss.Style
	subtleStyle     lipgloss.Style
	paginationStyle lipgloss.Style

	statusBarScrollPosStyle        func(...string) string
	statusBarNoteStyle             func(...string) string
	statusBarHelpStyle             func(...string) string
	statusBarMessageStyle          func(...string) string
	statusBarMessageHelpStyle      func(...string) string
	statusBarMessageScrollPosStyle func(...string) string
	lineNumberStyle                func(...string) string

	dividers     lipgloss.Style
	logoStyle    lipgloss.Style
	spinnerStyle lipgloss.Style

	tabStyle         lipgloss.Style
	selectedTabStyle lipgloss.Style

	selectorFg    color.Color
	hashFg        color.Color
	hashActiveFg  color.Color
	titleNormalFg color.Color
	titleActiveFg color.Color
	descNormalFg  color.Color
	descActiveFg  color.Color
	noContentFg   color.Color

	diffAdd       lipgloss.Style
	diffAddWord   lipgloss.Style
	diffDel       lipgloss.Style
	diffDelWord   lipgloss.Style
	diffContext   lipgloss.Style
	diffHunk      lipgloss.Style
	diffNoNewline lipgloss.Style
	diffBinary    lipgloss.Style
	diffFill      lipgloss.Style

	headerTab        lipgloss.Style
	headerTabActive  lipgloss.Style
	footerKeyFiles   lipgloss.Style
	footerKeyDiff    lipgloss.Style
	footerKeyCommits lipgloss.Style
	footerKeyCommit  lipgloss.Style
	footerValue      lipgloss.Style
	footerInfo       lipgloss.Style
	footerHelp       lipgloss.Style
	footerMessage    lipgloss.Style

	searchHi    lipgloss.Style
	searchHiSel lipgloss.Style

	scrollTrack lipgloss.Style
	scrollThumb lipgloss.Style

	// Modal chrome: the dimmed page behind a dialog and the dialog itself.
	modalBorder lipgloss.Style
	modalPanel  lipgloss.Style
	modalInput  lipgloss.Style
	modalTitle  lipgloss.Style
	modalHint   lipgloss.Style
	modalKeycap lipgloss.Style

	// Background is the whole-app background color (CharmTone Pepper).
	Background color.Color
	// BorderFocused and BorderBlurred color the pane borders.
	BorderFocused color.Color
	BorderBlurred color.Color
}

// New builds all styles on the CharmTone palette.
func New() Styles {
	var s Styles
	s.Background = tone.Pepper

	// Text colors on the Pepper background.
	fg := tone.Steep

	s.dimNormalFg = lipgloss.NewStyle().Foreground(tone.Squid).Render
	s.brightGrayFg = lipgloss.NewStyle().Foreground(tone.Steam).Render
	s.dimBrightGrayFg = lipgloss.NewStyle().Foreground(tone.Smoke).Render
	s.grayFg = lipgloss.NewStyle().Foreground(tone.Squid).Render
	s.midGrayFg = lipgloss.NewStyle().Foreground(tone.Steep).Render
	s.darkGrayFg = lipgloss.NewStyle().Foreground(tone.Iron)
	s.greenFg = lipgloss.NewStyle().Foreground(tone.Julep).Render
	s.semiDimGreenFg = lipgloss.NewStyle().Foreground(tone.Pickle).Render
	s.dimGreenFg = lipgloss.NewStyle().Foreground(tone.Gator).Render
	s.fuchsiaFg = lipgloss.NewStyle().Foreground(tone.Dolly).Render
	s.dimFuchsiaFg = lipgloss.NewStyle().Foreground(tone.Blush).Render
	s.dullFuchsiaFg = lipgloss.NewStyle().Foreground(tone.Macaron).Render
	s.redFg = lipgloss.NewStyle().Foreground(tone.Cherry).Render

	// Named styles.
	s.errorTitleStyle = lipgloss.NewStyle().
		Foreground(tone.Pepper).Background(tone.Cherry).Padding(0, 1)
	s.subtleStyle = lipgloss.NewStyle().Foreground(tone.Squid)
	s.paginationStyle = s.subtleStyle

	// Pager styles, kept for the search view.
	s.statusBarScrollPosStyle = lipgloss.NewStyle().
		Foreground(tone.Squid).Background(tone.BBQ).Render
	s.statusBarNoteStyle = lipgloss.NewStyle().
		Foreground(tone.Squid).Background(tone.BBQ).Render
	s.statusBarHelpStyle = lipgloss.NewStyle().
		Foreground(tone.Squid).Background(tone.Char).Render
	s.statusBarMessageStyle = lipgloss.NewStyle().
		Foreground(tone.Julep).Background(tone.Gator).Render
	s.statusBarMessageScrollPosStyle = s.statusBarMessageStyle
	s.statusBarMessageHelpStyle = lipgloss.NewStyle().
		Foreground(tone.Pepper).Background(tone.Julep).Render

	s.lineNumberStyle = lipgloss.NewStyle().Foreground(tone.Oyster).Render

	// Logo block: soft-serve's ServerName on the Charple ramp.
	s.dividers = darkGrayFgStyle(tone.Iron)
	s.logoStyle = lipgloss.NewStyle().
		Foreground(tone.Butter).Background(tone.Charple).Bold(true).Padding(0, 1)
	s.spinnerStyle = lipgloss.NewStyle().Foreground(tone.Hazy)

	// Tabs.
	s.tabStyle = lipgloss.NewStyle().Foreground(tone.Oyster)
	s.selectedTabStyle = lipgloss.NewStyle().Foreground(tone.Hazy)

	// List item styles: soft-serve's LogItem roles on CharmTone colors.
	s.selectorFg = tone.Dolly
	s.hashFg = tone.Orchid
	s.hashActiveFg = tone.Blush
	s.titleNormalFg = tone.Smoke
	s.titleActiveFg = tone.Sash
	s.descNormalFg = tone.Oyster
	s.descActiveFg = tone.Steam
	s.noContentFg = tone.Iron

	// Diff styles: Zed/VS Code-style washes on the CharmTone diff ramps.
	s.diffAdd = lipgloss.NewStyle().Foreground(tone.Julep).Background(tone.Spinach)
	s.diffAddWord = lipgloss.NewStyle().
		Bold(true).Foreground(tone.Bok).Background(tone.Gator)
	s.diffDel = lipgloss.NewStyle().Foreground(tone.Salmon).Background(tone.Toast)
	s.diffDelWord = lipgloss.NewStyle().
		Bold(true).Foreground(tone.Sash).Background(tone.Steak)
	s.diffContext = lipgloss.NewStyle().Foreground(fg)
	s.diffHunk = lipgloss.NewStyle().Foreground(tone.Hazy).Background(tone.BBQ)
	s.diffNoNewline = lipgloss.NewStyle().Foreground(tone.Oyster).Italic(true)
	s.diffBinary = lipgloss.NewStyle().Foreground(tone.Zest).Italic(true)
	// Split-view filler for the side that has no line on a paired row.
	s.diffFill = lipgloss.NewStyle().Foreground(tone.Iron)

	// Search highlights.
	s.searchHi = lipgloss.NewStyle().
		Background(tone.Zest).Foreground(tone.Pepper)
	s.searchHiSel = lipgloss.NewStyle().
		Bold(true).Background(tone.Bok).Foreground(tone.Pepper)

	// Scrollbars: a dim track with a bright thumb.
	s.scrollTrack = lipgloss.NewStyle().Foreground(tone.Iron)
	s.scrollThumb = lipgloss.NewStyle().Foreground(tone.Hazy).Bold(true)

	// Modal: the page behind a dialog is dimmed cell by cell; the dialog box
	// sits on the app background and only its editor area is a lifted panel.
	s.modalBorder = lipgloss.NewStyle().Foreground(tone.Hazy).Background(tone.Pepper)
	s.modalPanel = lipgloss.NewStyle().Background(tone.Pepper)
	s.modalInput = lipgloss.NewStyle().Background(tone.Char)
	s.modalTitle = lipgloss.NewStyle().
		Bold(true).
		Foreground(tone.Pepper).
		Background(tone.Hazy).
		Padding(0, 1)
	s.modalHint = lipgloss.NewStyle().Foreground(tone.Squid)
	s.modalKeycap = lipgloss.NewStyle().
		Bold(true).
		Foreground(tone.Pepper).
		Background(tone.Hazy).
		Padding(0, 1)

	// Segmented chrome: soft-serve's status bar roles on tone colors.
	// Header tabs are colored segments on the right side of the bar.
	s.headerTab = lipgloss.NewStyle().
		Foreground(tone.Squid).Background(tone.Char).Padding(0, 1)
	s.headerTabActive = lipgloss.NewStyle().
		Foreground(tone.Pepper).Background(tone.Hazy).Bold(true).Padding(0, 1)

	s.footerKeyFiles = lipgloss.NewStyle().
		Bold(true).Foreground(tone.Butter).Background(tone.Grape).Padding(0, 1)
	s.footerKeyDiff = lipgloss.NewStyle().
		Bold(true).Foreground(tone.Pepper).Background(tone.Dolly).Padding(0, 1)
	s.footerKeyCommits = lipgloss.NewStyle().
		Bold(true).Foreground(tone.Butter).Background(tone.Plum).Padding(0, 1)
	s.footerKeyCommit = lipgloss.NewStyle().
		Bold(true).Foreground(tone.Pepper).Background(tone.Orchid).Padding(0, 1)

	s.footerValue = lipgloss.NewStyle().
		Foreground(tone.Squid).Background(tone.BBQ)
	s.footerInfo = lipgloss.NewStyle().
		Foreground(tone.Pepper).Background(tone.Hazy).Padding(0, 1)
	s.footerHelp = lipgloss.NewStyle().
		Foreground(tone.Squid).Background(tone.Char).Padding(0, 1)
	s.footerMessage = lipgloss.NewStyle().
		Foreground(tone.Julep).Background(tone.Gator)

	// Pane borders.
	s.BorderFocused = tone.Hazy
	s.BorderBlurred = tone.Char

	return s
}

func darkGrayFgStyle(c color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(c)
}

// Exposed styles and render helpers for components.

// Logo renders the " Twig " wordmark block.
func (s Styles) Logo() string { return s.logoStyle.Render(" Twig ") }

// DividerDot renders the " • " divider.
func (s Styles) DividerDot() string { return s.dividers.Render(" • ") }

// DividerBar renders the " │ " divider.
func (s Styles) DividerBar() string { return s.dividers.Render(" │ ") }

// StatusBarScrollPos renders scroll percentage text.
func (s Styles) StatusBarScrollPos(text string) string { return s.statusBarScrollPosStyle(text) }

// StatusBarNote renders the note section of the status bar.
func (s Styles) StatusBarNote(text string) string { return s.statusBarNoteStyle(text) }

// StatusBarHelp renders the " ? Help " button.
func (s Styles) StatusBarHelp(text string) string { return s.statusBarHelpStyle(text) }

// StatusBarMessage renders a status message segment.
func (s Styles) StatusBarMessage(text string) string { return s.statusBarMessageStyle(text) }

// StatusBarMessageScrollPos renders the scroll percent in message mode.
func (s Styles) StatusBarMessageScrollPos(text string) string {
	return s.statusBarMessageScrollPosStyle(text)
}

// StatusBarMessageHelp renders the help button in message mode.
func (s Styles) StatusBarMessageHelp(text string) string { return s.statusBarMessageHelpStyle(text) }

// LineNumber renders a line number in the gutter.
func (s Styles) LineNumber(text string) string { return s.lineNumberStyle(text) }

// ErrorTitle renders the "ERROR" badge used by the error view.
func (s Styles) ErrorTitle() lipgloss.Style { return s.errorTitleStyle }

// Subtle renders dim, secondary text.
func (s Styles) Subtle() lipgloss.Style { return s.subtleStyle }

// Pagination renders pagination dots.
func (s Styles) Pagination() lipgloss.Style { return s.paginationStyle }

// SpinnerStyle styles the loading spinner.
func (s Styles) SpinnerStyle() lipgloss.Style { return s.spinnerStyle }

// Tab renders an unselected tab.
func (s Styles) Tab() lipgloss.Style { return s.tabStyle }

// SelectedTab renders the selected tab.
func (s Styles) SelectedTab() lipgloss.Style { return s.selectedTabStyle }

// SelectorFg is the list selector color.
func (s Styles) SelectorFg() color.Color { return s.selectorFg }

// HashFg is the hash color.
func (s Styles) HashFg() color.Color { return s.hashFg }

// HashActiveFg is the active hash color.
func (s Styles) HashActiveFg() color.Color { return s.hashActiveFg }

// TitleNormalFg is the normal item title color.
func (s Styles) TitleNormalFg() color.Color { return s.titleNormalFg }

// TitleActiveFg is the active item title color.
func (s Styles) TitleActiveFg() color.Color { return s.titleActiveFg }

// DescNormalFg is the normal item description color.
func (s Styles) DescNormalFg() color.Color { return s.descNormalFg }

// DescActiveFg is the active item description color.
func (s Styles) DescActiveFg() color.Color { return s.descActiveFg }

// NoContentFg is the empty-state color.
func (s Styles) NoContentFg() color.Color { return s.noContentFg }

// StatAddFg is the commit stats addition color.
func (s Styles) StatAddFg() color.Color { return tone.Julep }

// StatDelFg is the commit stats deletion color.
func (s Styles) StatDelFg() color.Color { return tone.Cherry }

// HeaderTab renders an inactive header bar tab.
func (s Styles) HeaderTab() lipgloss.Style { return s.headerTab }

// HeaderTabActive renders the active header bar tab.
func (s Styles) HeaderTabActive() lipgloss.Style { return s.headerTabActive }

// FooterKeyFiles renders the FILES key pill.
func (s Styles) FooterKeyFiles() lipgloss.Style { return s.footerKeyFiles }

// FooterKeyDiff renders the DIFF key pill.
func (s Styles) FooterKeyDiff() lipgloss.Style { return s.footerKeyDiff }

// FooterKeyCommits renders the COMMITS key pill.
func (s Styles) FooterKeyCommits() lipgloss.Style { return s.footerKeyCommits }

// FooterKeyCommit renders the COMMIT key pill.
func (s Styles) FooterKeyCommit() lipgloss.Style { return s.footerKeyCommit }

// FooterValue renders the flexible footer value segment.
func (s Styles) FooterValue() lipgloss.Style { return s.footerValue }

// FooterInfo renders the footer info pill.
func (s Styles) FooterInfo() lipgloss.Style { return s.footerInfo }

// FooterHelp renders the footer help pill.
func (s Styles) FooterHelp() lipgloss.Style { return s.footerHelp }

// ScrollTrack renders the unfilled part of a scrollbar.
func (s Styles) ScrollTrack(text string) string { return s.scrollTrack.Render(text) }

// ScrollThumb renders the filled part of a scrollbar.
func (s Styles) ScrollThumb(text string) string { return s.scrollThumb.Render(text) }

// FooterMessage renders the footer message segment.
func (s Styles) FooterMessage() lipgloss.Style { return s.footerMessage }

// DiffAdd returns the style for added lines.
func (s Styles) DiffAdd() lipgloss.Style { return s.diffAdd }

// DiffAddWord returns the style for changed words in added lines.
func (s Styles) DiffAddWord() lipgloss.Style { return s.diffAddWord }

// DiffDel returns the style for deleted lines.
func (s Styles) DiffDel() lipgloss.Style { return s.diffDel }

// DiffDelWord returns the style for changed words in deleted lines.
func (s Styles) DiffDelWord() lipgloss.Style { return s.diffDelWord }

// DiffContext returns the style for context lines.
func (s Styles) DiffContext() lipgloss.Style { return s.diffContext }

// DiffHunk returns the style for hunk headers.
func (s Styles) DiffHunk() lipgloss.Style { return s.diffHunk }

// DiffNoNewline returns the style for "no newline" markers.
func (s Styles) DiffNoNewline() lipgloss.Style { return s.diffNoNewline }

// DiffBinary returns the style for binary file notices.
func (s Styles) DiffBinary() lipgloss.Style { return s.diffBinary }

// DiffFill returns the style for empty split-view cells.
func (s Styles) DiffFill() lipgloss.Style { return s.diffFill }

// ModalBorder returns the dialog frame style.
func (s Styles) ModalBorder() lipgloss.Style { return s.modalBorder }

// ModalPanel returns the dialog's background style.
func (s Styles) ModalPanel() lipgloss.Style { return s.modalPanel }

// ModalInput returns the editor panel style (the one lifted area).
func (s Styles) ModalInput() lipgloss.Style { return s.modalInput }

// ModalTitle returns the dialog title style.
func (s Styles) ModalTitle() lipgloss.Style { return s.modalTitle }

// ModalHint returns the dialog hint style.
func (s Styles) ModalHint() lipgloss.Style { return s.modalHint }

// ModalKeycap returns the key cap style used in the dialog footer.
func (s Styles) ModalKeycap() lipgloss.Style { return s.modalKeycap }

// SearchHi returns the style for search matches.
func (s Styles) SearchHi() lipgloss.Style { return s.searchHi }

// SearchHiSel returns the style for the selected search match.
func (s Styles) SearchHiSel() lipgloss.Style { return s.searchHiSel }

// GreenFg renders text in the green accent.
func (s Styles) GreenFg(text string) string { return s.greenFg(text) }

// RedFg renders text in the red accent.
func (s Styles) RedFg(text string) string { return s.redFg(text) }

// GrayFg renders text in gray.
func (s Styles) GrayFg(text string) string { return s.grayFg(text) }

// MidGrayFg renders text in mid gray.
func (s Styles) MidGrayFg(text string) string { return s.midGrayFg(text) }

// BrightGrayFg renders text in bright gray.
func (s Styles) BrightGrayFg(text string) string { return s.brightGrayFg(text) }

// DimBrightGrayFg renders text in dim bright gray.
func (s Styles) DimBrightGrayFg(text string) string { return s.dimBrightGrayFg(text) }

// DimNormalFg renders text in dim normal.
func (s Styles) DimNormalFg(text string) string { return s.dimNormalFg(text) }

// FuchsiaFg renders text in the fuchsia accent.
func (s Styles) FuchsiaFg(text string) string { return s.fuchsiaFg(text) }

// DimFuchsiaFg renders text in dim fuchsia.
func (s Styles) DimFuchsiaFg(text string) string { return s.dimFuchsiaFg(text) }

// DullFuchsiaFg renders text in dull fuchsia.
func (s Styles) DullFuchsiaFg(text string) string { return s.dullFuchsiaFg(text) }

// SemiDimGreenFg renders text in semi-dim green.
func (s Styles) SemiDimGreenFg(text string) string { return s.semiDimGreenFg(text) }

// DimGreenFg renders text in dim green.
func (s Styles) DimGreenFg(text string) string { return s.dimGreenFg(text) }
