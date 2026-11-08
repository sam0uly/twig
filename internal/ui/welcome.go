package ui

import (
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"samouly.fun/twig/internal/ui/components/filelist"
)

const welcomeFrameInterval = 90 * time.Millisecond

// welcomeTickMsg advances the idle playground animation by one frame.
type welcomeTickMsg struct{}

func welcomeTick() tea.Cmd {
	return tea.Tick(welcomeFrameInterval, func(time.Time) tea.Msg {
		return welcomeTickMsg{}
	})
}

// welcomeView fills the body with a disposable, spring-driven pixel bonsai.
func (m model) welcomeView() string {
	status := "no commits yet"
	if m.files.Tab() == filelist.TabFiles {
		status = "no changes"
		if m.branch != "" {
			status = "working tree clean · " + m.branch
		}
	} else if m.branch != "" {
		status += " · " + m.branch
	}

	helpH := 0
	if m.help {
		helpH = m.helpHeight()
	}
	return m.playground.View(max(1, m.width), max(1, m.height-2-helpH), m.welcomeFrame, status)
}

// updateWelcomeKey keeps the idle artwork self-contained. Its controls never
// fall through to staging, commits, configuration, or the real repository.
func (m model) updateWelcomeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help = !m.help
		m.setSize()
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		m.message = ""
		return m.refresh()
	case key.Matches(msg, m.keys.FilesTab):
		return m.switchTab(filelist.TabFiles)
	case key.Matches(msg, m.keys.CommitsTab):
		return m.switchTab(filelist.TabCommits)
	}

	m.focus = focusMain
	// The playground's springs and sprites are values and fixed arrays. Mutating
	// this copied Tea model therefore cannot change the previous model's tree.
	m.playground.Update(msg)
	return m, nil
}

// preparePlaygroundInput removes hidden diff interactions before the artwork
// takes over. In particular, an active search must not consume bonsai controls.
func (m *model) preparePlaygroundInput() {
	m.focus = focusMain
	m.search.Deactivate()
	m.search.Clear()
	m.searchApplied = false
	m.view.ClearSearch()
	m.view.ClearReadSelection()
}

// clearEmptyCommitPane makes an empty commit log a genuinely empty pane and
// invalidates every asynchronous load belonging to the old commit selection.
func (m *model) clearEmptyCommitPane() {
	m.seq++
	m.commitSeq++
	m.commitFileList = nil
	m.files.SetCommitFiles(nil)
	m.preparePlaygroundInput()
	m.view.SetNotes(nil)
	m.view.SetFile(nil)
	m.d = nil
}

// playgroundInput reports whether a genuinely empty pane owns the keys. A
// synthetic diff hidden behind the artwork retains its old input behavior; real
// empty states clear their panes before bonsai input reaches this gate.
func (m model) playgroundInput() bool {
	return m.showWelcome() && m.view.Empty()
}

// showWelcome reports whether the pixel playground replaces the panes. Empty commits
// count only after their first load, so startup keeps its loading state instead.
func (m model) showWelcome() bool {
	if m.state != stateShowList || m.err != nil || !m.loaded {
		return false
	}
	switch m.files.Tab() {
	case filelist.TabFiles:
		return m.files.Total() == 0
	case filelist.TabCommits:
		return m.logLoaded && m.files.Total() == 0
	default:
		return false
	}
}
