package ui

import "charm.land/bubbles/v2/key"

// KeyMap holds every twig key binding, grouped by concern.
type KeyMap struct {
	Quit    key.Binding
	Help    key.Binding
	Refresh key.Binding

	// Listing.
	Accept   key.Binding
	Filter   key.Binding
	NextFile key.Binding
	PrevFile key.Binding

	// Diff navigation.
	PrevHunk key.Binding
	NextHunk key.Binding
	Top      key.Binding
	Bottom   key.Binding

	// Tab shortcuts.
	FilesTab   key.Binding
	CommitsTab key.Binding

	// Focus.
	Cycle     key.Binding
	CycleBack key.Binding
	Back      key.Binding

	// Search.
	Search     key.Binding
	SearchNext key.Binding
	SearchPrev key.Binding

	// Actions.
	Stage       key.Binding
	Unstage     key.Binding
	Commit      key.Binding
	Push        key.Binding
	YankFile    key.Binding
	YankHunk    key.Binding
	ToggleWrap  key.Binding
	ToggleWS    key.Binding
	CycleCtx    key.Binding
	ToggleView  key.Binding
	ToggleLeft  key.Binding
	ToggleRight key.Binding
	ToggleTree  key.Binding
	AllChanges  key.Binding
}

// DefaultKeyMap returns the default twig bindings.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r", "R", "ctrl+r"),
			key.WithHelp("r", "refresh"),
		),

		Accept: key.NewBinding(
			key.WithKeys("enter", "l", "right"),
			key.WithHelp("enter", "open"),
		),
		FilesTab: key.NewBinding(
			key.WithKeys("1"),
			key.WithHelp("1", "files"),
		),
		CommitsTab: key.NewBinding(
			key.WithKeys("2"),
			key.WithHelp("2", "commits"),
		),
		Cycle: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "pane"),
		),
		CycleBack: key.NewBinding(
			key.WithKeys("shift+tab"),
			key.WithHelp("S-tab", "prev pane"),
		),
		// Back deliberately excludes h, l and the arrow keys: those scroll
		// the diff horizontally, so only esc (and delete) leaves the pane.
		Back: key.NewBinding(
			key.WithKeys("esc", "delete"),
			key.WithHelp("esc", "back to files"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "find"),
		),
		NextFile: key.NewBinding(
			key.WithKeys("J"),
			key.WithHelp("J", "next file"),
		),
		PrevFile: key.NewBinding(
			key.WithKeys("K"),
			key.WithHelp("K", "prev file"),
		),

		PrevHunk: key.NewBinding(
			key.WithKeys("["),
			key.WithHelp("[", "prev hunk"),
		),
		NextHunk: key.NewBinding(
			key.WithKeys("]"),
			key.WithHelp("]", "next hunk"),
		),
		Top: key.NewBinding(
			key.WithKeys("g", "home"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("G", "bottom"),
		),

		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search"),
		),
		SearchNext: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "next match"),
		),
		SearchPrev: key.NewBinding(
			key.WithKeys("N"),
			key.WithHelp("N", "prev match"),
		),

		Stage: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "stage file"),
		),
		Unstage: key.NewBinding(
			key.WithKeys("u"),
			key.WithHelp("u", "unstage file"),
		),
		Commit: key.NewBinding(
			key.WithKeys("C"),
			key.WithHelp("C", "commit"),
		),
		Push: key.NewBinding(
			key.WithKeys("P"),
			key.WithHelp("P", "push"),
		),
		YankFile: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "yank patch"),
		),
		YankHunk: key.NewBinding(
			key.WithKeys("Y"),
			key.WithHelp("Y", "yank hunk"),
		),
		ToggleWrap: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("w", "wrap"),
		),
		ToggleWS: key.NewBinding(
			key.WithKeys("Z"),
			key.WithHelp("Z", "ignore ws"),
		),
		CycleCtx: key.NewBinding(
			key.WithKeys("c"),
			key.WithHelp("c", "context"),
		),
		ToggleView: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "split view"),
		),
		ToggleLeft: key.NewBinding(
			key.WithKeys("H"),
			key.WithHelp("H", "left sidebar"),
		),
		ToggleRight: key.NewBinding(
			key.WithKeys("L"),
			key.WithHelp("L", "right sidebar"),
		),
		ToggleTree: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "tree view"),
		),
		AllChanges: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "all changes"),
		),
	}
}
