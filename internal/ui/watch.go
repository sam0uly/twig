package ui

import (
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/watch"
)

// The poll backstop re-reads the working tree even when the watcher reports
// nothing, because a dropped event and a quiet repository look the same. Its
// interval scales with the cost of a status run, so a view that is cheap to
// refresh is refreshed often and an expensive one is not.
const (
	// pollFloorWatched is the backstop interval while events arrive live: the
	// poll only has to cover what a missed event would hide.
	pollFloorWatched = 10 * time.Second
	// pollFloorAlone is the interval where polling is the only signal twig
	// gets: no backend on this platform, a backend that failed, or one that
	// has stopped delivering.
	pollFloorAlone = 2 * time.Second
	pollCeiling    = time.Minute
	// suspectAfter is how long a watcher may stay silent while the poll keeps
	// finding changes before it stops being trusted.
	suspectAfter = 30 * time.Second
)

// watchChangedMsg reports filesystem activity around the repository.
type watchChangedMsg struct{}

// watchStartedMsg carries a live watcher, or the reason there is none.
type watchStartedMsg struct {
	w   *watch.Watcher
	err error
}

// statusPollMsg asks for a background status pass.
type statusPollMsg struct{}

// startWatchCmd creates a watcher for the current target set. It runs as a
// command like any other work, since it touches the kernel.
func startWatchCmd(dirs, files []string, quiet time.Duration) tea.Cmd {
	return func() tea.Msg {
		w, err := watch.New(watch.Options{Dirs: dirs, Files: files, Quiet: quiet})
		if err != nil {
			return watchStartedMsg{err: err}
		}
		return watchStartedMsg{w: w}
	}
}

// waitForChange parks on the watcher until it reports a change. Nothing is
// consumed while the repository is idle, so the wait costs one goroutine.
func waitForChange(w *watch.Watcher) tea.Cmd {
	return func() tea.Msg {
		if _, ok := <-w.Events(); !ok {
			return nil
		}
		return watchChangedMsg{}
	}
}

// statusTick schedules the next backstop pass.
func (m model) statusTick() tea.Cmd {
	if !m.cfg.Watch {
		return nil
	}
	return tea.Tick(pollInterval(m.watching(), m.statusCost), func(time.Time) tea.Msg {
		return statusPollMsg{}
	})
}

// pollInterval is how long to wait before re-reading the working tree: faster
// when polling is the only signal, and never faster than a status run is cheap
// enough to repeat.
func pollInterval(watched bool, cost time.Duration) time.Duration {
	every := pollFloorAlone
	if watched {
		every = pollFloorWatched
	}
	if scaled := 20 * cost; scaled > every {
		every = scaled
	}
	return min(every, pollCeiling)
}

// watching reports whether kernel events are actually arriving. A watcher that
// failed, that could not place part of its set, or that keeps missing changes
// the poll sees is the same as no watcher at all.
func (m model) watching() bool {
	return m.watchHealth.Live() && !m.watchSuspect
}

// watchTargets is the set whose changes matter: the git state, every listed
// file, the file on screen and its neighbours. Files are passed on every
// platform and only the ones that cannot see a content write through a
// directory watch use them.
func (m model) watchTargets() (dirs, files []string) {
	paths := m.watchPaths
	if fs, _ := m.files.SelectedFile(); fs.Path != "" {
		paths = append(slices.Clip(paths), fs.Path)
	}
	for _, e := range m.files.NeighborEntries(prefetchRadius) {
		paths = append(paths, e.Status.Path)
	}
	return git.WatchDirs(paths), git.WatchFiles(paths)
}

// watchQuiet is the configured quiet period, zero meaning the package default.
func (m model) watchQuiet() time.Duration {
	if m.cfg.WatchDebounce <= 0 {
		return 0
	}
	return time.Duration(m.cfg.WatchDebounce) * time.Millisecond
}

// watchCommands records the changed paths a status produced, then starts the
// watcher on the first load or moves its watch set to follow the view.
func (m *model) watchCommands(staged, unstaged []git.FileStatus) tea.Cmd {
	paths := make([]string, 0, len(staged)+len(unstaged))
	for _, f := range staged {
		paths = append(paths, f.Path)
	}
	for _, f := range unstaged {
		paths = append(paths, f.Path)
	}
	m.watchPaths = paths

	if !m.cfg.Watch {
		m.stopWatch()
		return nil
	}

	dirs, files := m.watchTargets()
	if m.watcher == nil {
		return m.startWatch(dirs, files)
	}
	m.watcher.Sync(dirs, files)
	m.watchHealth = m.watcher.Health()
	return nil
}

// startWatch brings a watcher up for the current view, if there is none.
func (m *model) startWatch(dirs, files []string) tea.Cmd {
	if !m.cfg.Watch || m.watcher != nil || m.watchOff || m.watchStarting {
		return nil
	}
	m.watchStarting = true
	return startWatchCmd(dirs, files, m.watchQuiet())
}

// syncWatch moves an existing watch set to follow the selection. It never
// starts a watcher: only a status load knows the repository is open.
func (m model) syncWatch() {
	if m.watcher == nil || !m.cfg.Watch {
		return
	}
	dirs, files := m.watchTargets()
	m.watcher.Sync(dirs, files)
}

// checkWatchTrust demotes a watcher that keeps missing changes. If the poll
// finds a change and nothing arrived from the backend for a while, the backend
// is installed but not delivering, which is exactly what a network mount or a
// broken platform backend looks like. Polling then runs at the fast interval
// and the footer says so.
func (m *model) checkWatchTrust(polled, changed bool) bool {
	if !polled || !changed || m.watchSuspect || !m.watchHealth.Live() {
		return false
	}
	if m.watchLastEvent.IsZero() || time.Since(m.watchLastEvent) < suspectAfter {
		return false
	}
	m.watchSuspect = true
	return true
}

// stopWatch closes the watcher. A wait parked on it returns nothing once the
// events channel closes, so nothing has to be cancelled.
func (m *model) stopWatch() {
	if m.watcher == nil {
		return
	}
	_ = m.watcher.Close()
	m.watcher = nil
	m.watchHealth = watch.Health{}
	m.watchSuspect = false
}

// requestStatus launches a status run unless one is already in flight, which
// keeps refreshes single-flight and their results in order. polled records that
// the periodic pass asked rather than a filesystem event.
func (m *model) requestStatus(polled bool) tea.Cmd {
	if m.statusPending {
		return nil
	}
	m.statusPending = true
	return loadStatusCmd(polled)
}

// refreshMode is the footer label for a view that kernel events are not
// carrying, empty when they are. It exists so the slow path is never silent.
func (m model) refreshMode() string {
	if !m.cfg.Watch {
		return ""
	}
	switch {
	case m.watchSuspect, m.watchOff:
		return "polling"
	case m.watcher != nil && !m.watchHealth.Live():
		return "polling"
	default:
		return ""
	}
}
