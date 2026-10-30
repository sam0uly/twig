package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/layout"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/config"
	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/ui/components/composer"
	"samouly.fun/twig/internal/ui/components/diffview"
	"samouly.fun/twig/internal/ui/components/filelist"
	"samouly.fun/twig/internal/ui/components/searchbar"
	"samouly.fun/twig/internal/ui/components/statusbar"
	"samouly.fun/twig/internal/ui/styles"
	"samouly.fun/twig/internal/watch"
)

const (
	statusMessageTimeout = time.Second * 3
	// prefetchRadius is how many neighbouring files are queued ahead of the
	// cursor while browsing the file list.
	prefetchRadius = 2
)

// Options configures the TUI.
type Options struct {
	Config config.Config
	// ConfigPath is the file twig read its config from and writes toggles
	// back to. It is also watched for edits made outside the app.
	ConfigPath    string
	Source        git.Source
	ShowLogPicker bool
}

type state int

const (
	stateShowList state = iota
	stateCompose
	stateFatal
)

type focus int

const (
	focusSidebar focus = iota
	focusMain
	focusCommitFiles
)

var contextCycle = []int{0, 1, 3, 5, 8}

// Run starts the twig TUI.
func Run(ctx context.Context, opts Options) error {
	// Opening the repository and resolving HEAD takes seconds on a large one, so
	// it happens in a command (see loadRepoInfoCmd): the first frame must never
	// wait for git.
	m := newModel(opts, "", "")

	p := tea.NewProgram(m, tea.WithContext(ctx))
	final, err := p.Run()
	// Release the inotify descriptor and the goroutines parked on it. The
	// process is about to exit, but a host that runs twig in-process (tests,
	// an embedded caller) must not accumulate them.
	if m, ok := final.(model); ok {
		m.stopWatch()
	}
	return err
}

// model is the root tea.Model.
type model struct {
	cfg     config.Config
	cfgPath string
	// cfgStamp fingerprints the config file, so an edit made outside twig is
	// picked up without restarting it.
	cfgStamp      string
	configTicking bool
	src           git.Source
	branch        string
	project       string

	width, height int
	styles        styles.Styles
	keys          KeyMap

	d *diff.Diff
	// loaded marks the first status load.
	loaded bool
	// lastLoaded tracks which file diff the main pane shows.
	lastLoaded struct {
		path   string
		staged bool
	}
	// reloadPending marks a refresh that has to re-read the file already on
	// screen, whose path did not move but whose content may have.
	reloadPending bool
	message       string
	// searchApplied keeps the query on screen (muted) after the input closes,
	// the same way a sidebar filter persists.
	searchApplied bool
	err           error
	state         state
	help          bool
	focus         focus

	// showLeft and showRight control the sidebar panes.
	showLeft  bool
	showRight bool

	// allChanges shows every working-tree change in one virtual document
	// instead of one file at a time.
	allChanges bool
	allLoading bool
	// allGen numbers the running all-changes load, so chunks from a
	// superseded load are dropped instead of appended to the new document.
	allGen int
	// allPendingJump defers the jump to the selected file until its band has
	// been received.
	allPendingJump bool
	// allAdds and allDels total the document's stats; the document itself
	// keeps only a window of rows, so it cannot be asked for them later.
	allAdds, allDels int
	// allAnchor is where the reader was when a refresh replaced the document,
	// and allRestore carries it to the end of the streamed rebuild. Both are
	// -1 when there is nothing to restore.
	allAnchor, allRestore int

	// welcomeFrame drives the welcome screen animation.
	welcomeFrame int
	// welcomeTicking records whether a welcome tick is in flight, so leaving
	// and re-entering the splash never stacks two chains.
	welcomeTicking bool
	// spinnerTicking records whether the spinner chain is running, so it is
	// started once per load and stops when the load lands.
	spinnerTicking bool
	// pushing marks a push in flight: it talks to the network, so the footer
	// shows the spinner until it returns.
	pushing bool

	// watcher reports filesystem activity around the repository. watchOff
	// records that there is no watcher to have, so it is never tried twice,
	// and watchStarting covers the command that is still creating one.
	watcher       *watch.Watcher
	watchOff      bool
	watchStarting bool
	// watchPaths are the changed paths the current watch set was built from.
	watchPaths []string
	// watchHealth is what the watcher is actually doing: live events, a partial
	// set, or nothing. The footer shows it and the poll interval follows it.
	watchHealth watch.Health
	// watchSuspect marks a watcher that is installed but keeps missing changes
	// the poll finds, which is how a network mount or a broken backend looks.
	watchSuspect bool
	// watchLastEvent is when the watcher last reported activity, used to tell
	// a real event from a change only the poll noticed.
	watchLastEvent time.Time
	// statusPending marks a status run in flight, so a tick or an event
	// cannot stack a second one behind it.
	statusPending bool
	// repoFingerprint is the working tree state last painted. A refresh that
	// produces the same fingerprint is dropped without touching the view.
	repoFingerprint string
	// statusCost is how long the last status run took, which sets how often
	// the poll backstop runs.
	statusCost time.Duration

	// commit is the commit inspected on the commits tab.
	commit git.Commit
	// commitFileList holds the files of that commit for the right sidebar.
	commitFileList []*diff.File

	// cache holds recently generated diffs so navigating back is instant.
	cache *diffCache
	// prefetchAt is the file index whose neighbours are already queued.
	prefetchAt int
	// seq numbers the latest file selection; stale loads are dropped instead of
	// repainting the pane with a diff the user already moved past.
	seq int
	// commitSeq numbers the latest commit selection. It is kept apart from seq
	// so a commit load can never discard a file diff that is still wanted (and
	// the other way round).
	commitSeq int

	files   filelist.Model
	view    diffview.Model
	status  statusbar.Model
	search  searchbar.Model
	compose composer.Model
	spinner spinner.Model

	// pane rects, computed by setSize.
	sideRect       uv.Rectangle
	mainRect       uv.Rectangle
	commitFilesRct uv.Rectangle
}

func newModel(opts Options, branch, project string) model {
	st := styles.New()

	m := model{
		cache:      newDiffCache(cacheBytes),
		prefetchAt: -1,
		allAnchor:  -1,
		allRestore: -1,
		// The sidebars follow the config, and H/L flip them from there: the
		// diff gets the whole window unless one was asked for.
		showLeft:  opts.Config.ShowLeftSidebar,
		showRight: opts.Config.ShowRightSidebar,
		cfg:       opts.Config,
		cfgPath:   opts.ConfigPath,
		cfgStamp:  configStamp(opts.ConfigPath),
		src:       opts.Source,
		branch:    branch,
		project:   project,
		styles:    st,
		keys:      DefaultKeyMap(),
		state:     stateShowList,
		focus:     focusMain,
		files:     filelist.New(0, 0, st),
		view:      diffview.New(opts.Config.SmoothScroll, st),
		status:    statusbar.New(st),
		search:    searchbar.New(st),
		compose:   composer.New(st),
		spinner:   spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(st.SpinnerStyle())),
	}
	m.view.SetWrap(opts.Config.Wrap)
	m.view.SetLineNumbers(opts.Config.LineNumbers)
	m.view.SetSplit(opts.Config.SplitView)
	if opts.ShowLogPicker {
		m.files.SetTab(filelist.TabCommits)
	}
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.RequestBackgroundColor,
		loadRepoInfoCmd(),
		loadStatusCmd(false),
		loadLogCmd(m.cfg.LogLimit),
		configTick(),
		m.statusTick(),
	)
}

func (m model) diffOptions() git.Options {
	return git.Options{Context: m.cfg.Context, IgnoreWhitespace: m.cfg.IgnoreWhitespace}
}

// Update implements tea.Model. It also keeps the welcome animation alive:
// leaving the splash for another tab ends the tick chain, and the transition
// back into it starts a fresh one. A message that lands on an already visible
// splash is left alone, so its command keeps its identity.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	wasWelcome := m.showWelcome()
	next, cmd := m.update(msg)
	nm, ok := next.(model)
	if !ok {
		return next, cmd
	}
	if !wasWelcome && nm.showWelcome() && !nm.welcomeTicking {
		nm.welcomeTicking = true
		cmd = tea.Batch(cmd, welcomeTick())
	}
	// The spinner runs only while there is work in front of the user: the
	// first load, and a push. Its chain ends when that work lands.
	if (nm.loading() || nm.pushing) && !nm.spinnerTicking {
		nm.spinnerTicking = true
		cmd = tea.Batch(cmd, nm.spinner.Tick)
	}
	return nm, cmd
}

// isSpinnerTick reports whether msg is a spinner frame.
func isSpinnerTick(msg tea.Msg) bool {
	_, ok := msg.(spinner.TickMsg)
	return ok
}

func (m model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.err != nil && m.state == stateFatal {
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return m, tea.Quit
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.applyStyles(styles.New())
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.setSize()
		return m, nil

	case statusLoadedMsg:
		m.statusPending = false
		if msg.err != nil {
			m.err = msg.err
			m.state = stateFatal
			return m, nil
		}
		m.statusCost = msg.cost
		watchCmd := m.watchCommands(msg.staged, msg.unstaged)

		unchanged := m.loaded && msg.fingerprint != "" && msg.fingerprint == m.repoFingerprint
		if m.checkWatchTrust(msg.polled, !unchanged) {
			// The poll keeps finding changes the watcher never reported, so it
			// is installed but not delivering: say so once and let the faster
			// poll take over.
			m.message = "live refresh unavailable, polling"
			watchCmd = tea.Batch(watchCmd, setMessageCmd(m.message))
		}
		if unchanged {
			// Nothing about the working tree changed, so there is nothing to
			// rebuild: the selection and scroll position survive a refresh
			// that found the same state.
			return m, watchCmd
		}
		m.repoFingerprint = msg.fingerprint
		// The working tree changed: previously generated file diffs are stale.
		m.cache.Clear()
		m.loaded = true
		m.files.SetFiles(msg.staged, msg.unstaged)
		if m.files.Tab() == filelist.TabFiles && m.files.Total() == 0 {
			// Everything was staged or reverted: drop the stale diff so the
			// welcome screen can take over.
			m.allChanges = false
			m.allLoading = false
			m.allPendingJump = false
			m.allAdds, m.allDels = 0, 0
			m.view.SetFile(nil)
			m.d = nil
			m.lastLoaded.path, m.lastLoaded.staged = "", false
		}
		// The file on screen may be exactly the one that changed, and its
		// path did not move: reload it instead of trusting the cursor.
		if m.lastLoaded.path != "" && m.files.Tab() == filelist.TabFiles {
			m.reloadPending = true
		}
		if m.allChanges && m.files.Tab() == filelist.TabFiles {
			// The rebuild replaces the whole document, so remember the line
			// the reader was on and open the new one there.
			m.allAnchor = m.view.Anchor()
			next, cmd := m.startAllChanges()
			return next, tea.Batch(cmd, watchCmd)
		}
		next, cmd := m.autoLoadFile()
		return next, tea.Batch(cmd, watchCmd)

	case repoInfoMsg:
		m.branch, m.project = msg.branch, msg.project
		return m, nil

	case logLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.state = stateFatal
			return m, nil
		}
		m.files.SetCommits(msg.commits)
		if m.files.Tab() != filelist.TabCommits {
			// The files tab owns the pane. Opening a commit here would race the
			// file diff the reader is waiting for, and would bump the sequence
			// that decides whether that diff is still wanted.
			return m, nil
		}
		if c, ok := m.files.SelectedCommit(); ok {
			return m.showCommit(c)
		}
		return m, nil

	case configTickMsg:
		return m.watchConfig()

	case watchStartedMsg:
		m.watchStarting = false
		if msg.err != nil || msg.w == nil || !m.cfg.Watch {
			// No watcher is not a failure: the poll backstop covers the
			// repository without one, so this is only worth remembering. A
			// watcher that arrived after watching was turned off is closed
			// rather than parked on.
			if msg.w != nil {
				_ = msg.w.Close()
			}
			m.watchOff = msg.err != nil
			return m, nil
		}
		m.watcher = msg.w
		// The set was captured before the watcher existed, and the view may
		// have moved on since: sync before waiting, or the first events for
		// newly listed targets are missed.
		dirs, files := m.watchTargets()
		m.watcher.Sync(dirs, files)
		m.watchHealth = m.watcher.Health()
		return m, waitForChange(m.watcher)

	case watchChangedMsg:
		if m.watcher == nil {
			return m, nil
		}
		m.watchLastEvent = time.Now()
		return m, tea.Batch(waitForChange(m.watcher), m.requestStatus(false))

	case statusPollMsg:
		// The poll chain re-arms here and only here, so exactly one chain is
		// alive however many status runs it triggers.
		cmds := []tea.Cmd{m.statusTick()}
		if m.loaded {
			cmds = append(cmds, m.requestStatus(true))
		}
		return m, tea.Batch(cmds...)

	case tea.FocusMsg:
		// Back from another window, which is where changes made elsewhere
		// usually come from: catch up, once.
		if !m.loaded || !m.cfg.Watch {
			return m, nil
		}
		return m, m.requestStatus(false)

	case welcomeTickMsg:
		if !m.showWelcome() {
			m.welcomeTicking = false
			return m, nil
		}
		m.welcomeFrame++
		return m, welcomeTick()

	case allLoadedMsg:
		m.allLoading = false
		if msg.err != nil {
			m.message = msg.err.Error()
			return m, setMessageCmd(m.message)
		}
		if !m.allChanges || msg.file == nil {
			return m, nil
		}
		m.d = &diff.Diff{Additions: msg.file.Additions, Deletions: msg.file.Deletions}
		m.view.SetNotes(nil)
		m.view.SetFile(msg.file)
		m.view.JumpToBandNow(m.files.SelectedOrdinal() - 1)
		return m, nil

	case allChunkMsg:
		if msg.gen != m.allGen {
			return m, nil
		}
		if msg.err != nil {
			m.allLoading = false
			m.message = msg.err.Error()
			return m, setMessageCmd(m.message)
		}
		if !m.allChanges || m.files.Tab() != filelist.TabFiles {
			m.allLoading = false
			return m, nil
		}

		first := msg.from == 0
		m.view.AppendBlocks(msg.files)
		for _, f := range msg.files {
			m.allAdds += f.Additions
			m.allDels += f.Deletions
		}
		m.d = &diff.Diff{Additions: m.allAdds, Deletions: m.allDels}

		// The selected file may sit beyond the blocks received so far; keep
		// the jump pending until its band exists.
		if first || m.allPendingJump {
			idx := m.files.SelectedOrdinal() - 1
			if m.view.HasBand(idx) {
				m.view.JumpToBandNow(idx)
				m.allPendingJump = false
			} else {
				m.allPendingJump = true
			}
		}

		if !msg.done {
			m.allLoading = true
			return m, loadAllChunkCmd(msg.entries, msg.from+len(msg.files), msg.gen, m.diffOptions())
		}
		m.allLoading = false
		if m.allRestore >= 0 {
			// The document was rebuilt by a refresh rather than by the reader:
			// put them back on the line they were reading instead of leaving
			// the pane at the file the sidebar happens to select.
			m.view.JumpToLine(m.allRestore)
			m.allRestore = -1
		}
		return m, nil

	case prefetchedMsg:
		if msg.err == nil && msg.file != nil {
			m.cache.Put(msg.key, cacheEntry{file: msg.file})
		}
		return m, nil

	case diffview.SearchResultsMsg:
		if !m.view.ApplySearchResults(msg) {
			// A newer search replaced this one, or the search was cleared.
			return m, nil
		}
		if len(msg.Matches) == 0 {
			m.message = "no matches"
		} else {
			m.message = ""
		}
		return m, setMessageCmd(m.message)

	case fileLoadedMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
			if msg.seq == m.seq {
				// The pane must not keep showing a diff the reader has moved
				// past: say what failed, where the diff would have been.
				m.view.SetNotes(nil)
				m.view.SetFile(diff.Unavailable(msg.path, msg.err.Error()))
				m.d = nil
				m.lastLoaded.path, m.lastLoaded.staged = "", false
			}
			return m, setMessageCmd(m.message)
		}
		m.cache.Put(fileCacheKey(msg.path, msg.staged, m.cfg), cacheEntry{file: msg.file})
		if msg.seq != m.seq && msg.seq != 0 {
			// The user moved on; the diff is cached for when they come back.
			return m, nil
		}
		if m.lastLoaded.path == msg.path && m.lastLoaded.staged == msg.staged {
			// A refresh reloaded the file the reader is on. The content-addressed
			// diff cache hands back the same object when the file did not change,
			// so nothing moved and the pane is left alone; when it did change,
			// the diff is redrawn where they were reading.
			if m.view.File() == msg.file {
				return m, nil
			}
			m.view.SetFileAt(msg.file, m.view.Anchor())
			m.d = &diff.Diff{Additions: msg.file.Additions, Deletions: msg.file.Deletions}
			return m, nil
		}
		m.view.SetNotes(nil)
		m.view.SetFile(msg.file)
		m.d = &diff.Diff{Additions: msg.file.Additions, Deletions: msg.file.Deletions}
		m.lastLoaded.path, m.lastLoaded.staged = msg.path, msg.staged
		return m, nil

	case commitListMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
			if msg.seq == m.commitSeq {
				m.view.SetNotes(nil)
				m.view.SetFile(diff.Unavailable(msg.commit.Short, msg.err.Error()))
				m.d = nil
			}
			return m, setMessageCmd(m.message)
		}
		if msg.seq != m.commitSeq || msg.seq == 0 {
			return m, nil
		}
		if len(msg.changes) == 0 {
			m.message = "no changes"
			return m, setMessageCmd(m.message)
		}
		if len(msg.changes) > commitEagerFiles {
			m.applyCommitLazy(msg.commit, msg.changes)
			return m, loadCommitChunkCmd(msg.commit, msg.changes, 0, msg.seq, m.diffOptions())
		}
		return m, loadCommitDiffCmd(msg.commit, m.diffOptions(), msg.seq)

	case commitChunkMsg:
		if msg.seq != m.commitSeq {
			return m, nil
		}
		if msg.err != nil {
			m.message = msg.err.Error()
			return m, setMessageCmd(m.message)
		}
		m.view.AppendBlocks(msg.files)
		for i, f := range msg.files {
			if j := msg.from + i; j < len(m.commitFileList) && m.commitFileList[j] != nil {
				m.commitFileList[j].Additions, m.commitFileList[j].Deletions = f.Additions, f.Deletions
			}
			if m.d != nil {
				m.d.Additions += f.Additions
				m.d.Deletions += f.Deletions
			}
		}
		if next := msg.from + len(msg.files); next < len(msg.changes) {
			return m, loadCommitChunkCmd(msg.commit, msg.changes, next, msg.seq, m.diffOptions())
		}
		return m, nil

	case commitLoadedMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
			if msg.seq == m.commitSeq {
				// The pane must not keep showing whatever came before: the
				// reader asked for this commit and it could not be built.
				m.view.SetNotes(nil)
				m.view.SetFile(diff.Unavailable(msg.commit.Short, msg.err.Error()))
				m.d = nil
			}
			return m, setMessageCmd(m.message)
		}
		// Cache the commit without its per-file payload: the sidebar only
		// needs paths and counts, and the aggregated document already holds
		// every row the viewer will paint.
		m.cache.Put(commitCacheKey(msg.commit.Hash, m.cfg), cacheEntry{file: msg.file, files: slimFiles(msg.files)})
		if msg.seq != m.commitSeq && msg.seq != 0 {
			return m, nil
		}
		m.applyCommit(msg.commit, msg.file, msg.files)
		if c, ok := m.files.SelectedCommit(); ok && c.Hash != msg.commit.Hash {
			// A newer selection arrived while we were loading; follow it.
			return m.showCommit(c)
		}
		return m, nil

	case stageDoneMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
			return m, setMessageCmd(m.message)
		}
		if msg.staged {
			m.message = "staged!"
		} else {
			m.message = "unstaged!"
		}
		return m, tea.Batch(loadStatusCmd(false), setMessageCmd(m.message))

	case committedMsg:
		if msg.err != nil {
			m.message = msg.err.Error()
			return m, setMessageCmd(m.message)
		}
		m.state = stateShowList
		m.message = "committed " + msg.hash
		return m, tea.Batch(m.requestStatus(false), loadLogCmd(m.cfg.LogLimit), setMessageCmd(m.message))

	case pushedMsg:
		m.pushing = false
		if msg.err != nil {
			m.message = msg.err.Error()
		} else {
			m.message = "pushed!"
		}
		return m, setMessageCmd(m.message)

	case tea.ClipboardMsg:
		m.message = "copied to clipboard!"
		return m, setMessageCmd(m.message)

	case clearMsgMsg:
		m.message = ""
		return m, nil

	case tea.MouseWheelMsg:
		return m.updateWheel(msg)

	case tea.MouseClickMsg:
		return m.updateClick(msg)

	case tea.KeyPressMsg:
		return m.updateKey(msg)

	case list.FilterMatchesMsg:
		// The matches belong to the list that asked for them. The commit-file
		// sidebar is not the visible tab's list, so without this the results
		// landed in the commits listing and the right pane never narrowed.
		if m.files.CommitFilesFiltering() {
			var cmd tea.Cmd
			m.files, cmd = m.files.UpdateCommitFiles(msg)
			return m, cmd
		}
		var cmd tea.Cmd
		m.files, cmd = m.files.Update(msg)
		return m, cmd
	}

	return m.updateChildren(msg)
}

// autoLoadFile loads the diff of the currently selected file so the main
// pane is never stale after a status refresh.
func (m model) autoLoadFile() (tea.Model, tea.Cmd) {
	if m.files.Tab() != filelist.TabFiles || m.allChanges {
		return m, nil
	}
	fs, staged := m.files.SelectedFile()
	if fs.Path == "" {
		return m, nil
	}
	// A refresh re-reads the file on screen even though its path did not
	// move, because its content may have.
	if !m.reloadPending && fs.Path == m.lastLoaded.path && staged == m.lastLoaded.staged && m.lastLoaded.path != "" {
		return m, nil
	}
	m.reloadPending = false
	return m.showFile(fs.Path, staged)
}

// showFile displays a file diff, reusing the cached copy when available and
// otherwise loading it right away.
func (m model) showFile(path string, staged bool) (tea.Model, tea.Cmd) {
	// The file on screen is the one the reader is about to edit, so its
	// directory joins the watch set before anything is loaded.
	m.syncWatch()
	if e, ok := m.cache.Get(fileCacheKey(path, staged, m.cfg)); ok {
		m.view.SetNotes(nil)
		m.view.SetFile(e.file)
		m.d = &diff.Diff{Additions: e.file.Additions, Deletions: e.file.Deletions}
		m.lastLoaded.path, m.lastLoaded.staged = path, staged
		return m, nil
	}
	// File diffs are cheap: load immediately so holding an arrow key keeps
	// the main pane in step with the selection.
	m.seq++
	return m, loadFileDiffCmd(path, staged, m.diffOptions(), m.seq)
}

// showCommit displays a commit diff, reusing the cached copy when available
// and otherwise loading it right away.
func (m model) showCommit(c git.Commit) (tea.Model, tea.Cmd) {
	if e, ok := m.cache.Get(commitCacheKey(c.Hash, m.cfg)); ok {
		m.applyCommit(c, e.file, e.files)
		return m, nil
	}
	// Listing the commit is tree data only, so it is cheap even where the
	// content is not present locally; how much to load follows from the list.
	m.commitSeq++
	return m, loadCommitListCmd(c, m.commitSeq)
}

// prefetchNeighbors warms the cache for the files just above and below the
// cursor, so stepping through a long listing is instant.
func (m model) prefetchNeighbors() (model, tea.Cmd) {
	if m.files.Tab() != filelist.TabFiles {
		return m, nil
	}
	idx := m.files.SelectedIndex()
	if idx == m.prefetchAt {
		return m, nil
	}
	m.prefetchAt = idx

	opts := m.diffOptions()
	var cmds []tea.Cmd
	for _, e := range m.files.NeighborEntries(prefetchRadius) {
		key := fileCacheKey(e.Status.Path, e.Staged, m.cfg)
		if _, ok := m.cache.Get(key); ok {
			continue
		}
		path, staged := e.Status.Path, e.Staged
		cmds = append(cmds, func() tea.Msg {
			f, err := git.FileDiff(path, staged, opts)
			return prefetchedMsg{key: key, file: f, err: err}
		})
	}
	return m, tea.Batch(cmds...)
}

// applyCommit shows a loaded commit diff and its file list.
func (m *model) applyCommit(c git.Commit, f *diff.File, files []*diff.File) {
	m.commit = c
	m.commitFileList = files
	m.d = &diff.Diff{Additions: f.Additions, Deletions: f.Deletions}
	m.files.SetCommitFiles(files)
	m.view.SetNotes(commitNotes(m.styles, c, files))
	m.view.SetFile(f)
}

// applyCommitLazy shows a large commit as a virtual document. The file list came
// from tree data, so nothing was diffed yet: each file is generated when it
// comes into view (and again if it is evicted), the sidebar fills in its counts
// as files arrive, and a file whose content is missing locally shows a one-line
// band instead of failing the commit.
func (m *model) applyCommitLazy(c git.Commit, changes []git.CommitChange) {
	skeleta := commitSkeleta(changes)
	m.commit = c
	m.commitFileList = skeleta
	m.d = &diff.Diff{}
	m.files.SetCommitFiles(skeleta)
	m.view.SetNotes(commitNotes(m.styles, c, skeleta))
	m.view.SetDocument(commitBlockLoader(c.Hash, changes, m.diffOptions()))
}

// commitSkeleta is the sidebar payload for a lazily loaded commit: paths and
// kinds, with the counts left unknown (negative) until the diff is loaded.
func commitSkeleta(changes []git.CommitChange) []*diff.File {
	out := make([]*diff.File, len(changes))
	for i, c := range changes {
		out[i] = &diff.File{
			OldPath:   c.OldPath,
			NewPath:   c.Path,
			Kind:      c.Kind,
			Additions: -1,
			Deletions: -1,
		}
	}
	return out
}

// commitBlockLoader re-diffs one file of a lazily loaded commit when the
// document needs its rows again. The session's diff cache makes that a lookup in
// the common case.
func commitBlockLoader(hash string, changes []git.CommitChange, opts git.Options) func(int) *diff.File {
	return func(i int) *diff.File {
		if i < 0 || i >= len(changes) {
			return nil
		}
		f, err := git.FileAtCommit(hash, changes[i], opts)
		if err != nil {
			return diff.Unavailable(changes[i].Path, err.Error())
		}
		return f
	}
}

// slimFiles strips the content of a commit's per-file diffs. Their paths and
// counts are all the per-commit sidebar renders, and the cached document
// already holds the rows.
func slimFiles(files []*diff.File) []*diff.File {
	out := make([]*diff.File, len(files))
	for i, f := range files {
		out[i] = f.Slim()
	}
	return out
}

func (m model) updateChildren(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	// The spinner runs only while the first diff is on its way. Every frame
	// repaints the whole view, so an idle twig keeps the chain stopped.
	if m.loading() || m.pushing || !isSpinnerTick(msg) {
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		m.spinnerTicking = false
	}

	switch m.state {
	case stateCompose:
		m.compose, cmd = m.compose.Update(msg)
		cmds = append(cmds, cmd)
	default:
		m.view, cmd = m.view.Update(msg)
		cmds = append(cmds, cmd)
		m.files, cmd = m.files.Update(msg)
		cmds = append(cmds, cmd)
	}

	m.search, cmd = m.search.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m model) updateWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.state != stateShowList {
		return m, nil
	}
	switch {
	case m.showLeft && inRect(msg.X, msg.Y, m.sideRect):
		var cmd tea.Cmd
		m.files, cmd = m.files.Update(msg)
		// The wheel moves the list cursor too; keep the diff in step.
		return m.followSelection(cmd)
	case m.showRight && m.files.Tab() == filelist.TabCommits && inRect(msg.X, msg.Y, m.commitFilesRct):
		var cmd tea.Cmd
		m.files, cmd = m.files.UpdateCommitFiles(msg)
		return m, cmd
	default:
		var cmd tea.Cmd
		m.view, cmd = m.view.Update(msg)
		return m, cmd
	}
}

// updateClick handles mouse clicks across the whole screen: header tabs,
// the help pill, pane focus, and row selection inside the sidebars.
func (m model) updateClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if m.state != stateShowList {
		return m, nil
	}

	// Header: the numbered tab segments switch tabs.
	if msg.Y == 0 {
		filesX, filesW, commitsX, commitsW := m.tabHitRanges()
		switch {
		case msg.X >= filesX && msg.X < filesX+filesW:
			return m.switchTab(filelist.TabFiles)
		case msg.X >= commitsX && msg.X < commitsX+commitsW:
			return m.switchTab(filelist.TabCommits)
		}
		return m, nil
	}

	// Footer: the help pill toggles the help panel.
	if msg.Y == m.height-1 {
		if msg.X >= m.width-lipgloss.Width(m.styles.FooterHelp().Render(" ? Help ")) {
			m.help = !m.help
			m.setSize()
			return m, nil
		}
		return m, nil
	}

	if m.showLeft && inRect(msg.X, msg.Y, m.sideRect) {
		row := msg.Y - m.sideRect.Min.Y - 1 // skip the border
		// the commits tab lists commits where the files tab lists files, and
		// clicking either one follows the selection into the main pane
		if m.files.Tab() == filelist.TabCommits {
			idx, ok := m.files.CommitAtRow(row)
			if !ok {
				return m, nil
			}
			m.files.SelectCommit(idx)
			m.focus = focusSidebar
			return m.followSelection(nil)
		}
		idx, ok := m.files.FileAtRow(row)
		if !ok {
			return m, nil
		}
		m.files.Select(idx)
		m.focus = focusSidebar
		if m.files.ToggleSelectedDir() {
			return m, nil
		}
		return m.followSelection(nil)
	}

	if m.showRight && m.files.Tab() == filelist.TabCommits && inRect(msg.X, msg.Y, m.commitFilesRct) {
		row := msg.Y - m.commitFilesRct.Min.Y - 1
		idx, ok := m.files.CommitFileAt(row)
		if !ok {
			return m, nil
		}
		m.files.SelectCommitFile(idx)
		m.focus = focusCommitFiles
		m.view.JumpToBandNow(idx)
		return m, nil
	}

	if inRect(msg.X, msg.Y, m.mainRect) {
		m.focus = focusMain
		return m, nil
	}

	return m, nil
}

// tabHitRanges returns the cell ranges of the header tab segments. The ranges
// are read from the rendered header itself so they always match what is on
// screen, whatever the styles do.
func (m model) tabHitRanges() (filesX, filesW, commitsX, commitsW int) {
	header := ansi.Strip(m.headerView())

	idx := strings.Index(header, "1 Files")
	if idx < 0 {
		return 0, 0, 0, 0
	}
	filesX, filesW = max(0, idx-1), len("1 Files")+2

	idx = strings.Index(header, "2 Commits")
	if idx < 0 {
		return filesX, filesW, 0, 0
	}
	commitsX, commitsW = max(0, idx-1), len("2 Commits")+2
	return filesX, filesW, commitsX, commitsW
}

// inRect reports whether a cell lies inside a pane body (borders excluded).
func inRect(x, y int, r uv.Rectangle) bool {
	return x > r.Min.X && x < r.Max.X-1 && y > r.Min.Y && y < r.Max.Y-1
}

func (m model) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Esc clears a search or a filter; it never moves focus around.
	if key.Matches(msg, m.keys.Back) {
		switch {
		case m.search.Active() || m.searchApplied:
			m.search.Deactivate()
			m.search.Clear()
			m.searchApplied = false
			m.view.ClearSearch()
			m.setSize()
			return m, nil
		case m.focus == focusCommitFiles && m.files.CommitFilesFiltered():
			var cmd tea.Cmd
			m.files, cmd = m.files.UpdateCommitFiles(msg)
			return m, cmd
		case m.focus == focusSidebar && m.files.SidebarFiltered():
			var cmd tea.Cmd
			m.files, cmd = m.files.Update(msg)
			return m, cmd
		}
	}

	// While the sidebar list is filtering, every key belongs to it.
	if m.state == stateShowList && m.focus == focusSidebar && m.files.FilterActive() {
		var cmd tea.Cmd
		m.files, cmd = m.files.Update(msg)
		return m, cmd
	}
	if m.state == stateShowList && m.focus == focusCommitFiles && m.files.CommitFilesFiltering() {
		var cmd tea.Cmd
		m.files, cmd = m.files.UpdateCommitFiles(msg)
		return m, cmd
	}

	if m.search.Active() {
		return m.updateSearch(msg)
	}
	if m.state == stateFatal {
		return m, tea.Quit
	}
	if m.state == stateCompose {
		return m.updateCompose(msg)
	}

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
	case key.Matches(msg, m.keys.NextHunk):
		return m, m.view.NextHunk()
	case key.Matches(msg, m.keys.PrevHunk):
		return m, m.view.PrevHunk()
	case key.Matches(msg, m.keys.NextFile):
		return m.stepFile(1)
	case key.Matches(msg, m.keys.PrevFile):
		return m.stepFile(-1)
	case key.Matches(msg, m.keys.ToggleLeft):
		m.showLeft = !m.showLeft
		if !m.showLeft && m.focus == focusSidebar {
			m.focus = focusMain
		}
		m.setSize()
		m.cfg.ShowLeftSidebar = m.showLeft
		m.message = "left sidebar " + onOff(m.showLeft)
		m.saveSetting()
		return m, setMessageCmd(m.message)
	case key.Matches(msg, m.keys.ToggleRight):
		m.showRight = !m.showRight
		if !m.showRight && m.focus == focusCommitFiles {
			m.focus = focusMain
		}
		m.setSize()
		m.cfg.ShowRightSidebar = m.showRight
		m.message = "right sidebar " + onOff(m.showRight)
		m.saveSetting()
		return m, setMessageCmd(m.message)
	case key.Matches(msg, m.keys.ToggleTree):
		m.files.ToggleTree()
		m.message = "tree view " + onOff(m.files.Tree())
		return m, setMessageCmd(m.message)
	case key.Matches(msg, m.keys.ToggleView):
		m.view.ToggleSplit()
		m.cfg.SplitView = m.view.Split()
		// Wrap is a stacked-layout feature: the two columns of a split view
		// are already narrow, and a wrapped column would need row heights the
		// layout does not have.
		if m.cfg.SplitView && m.cfg.Wrap {
			m.cfg.Wrap = false
			m.view.SetWrap(false)
		}
		m.message = "view " + viewMode(m.cfg.SplitView)
		m.saveSetting()
		return m, setMessageCmd(m.message)
	case key.Matches(msg, m.keys.ToggleWrap):
		m.cfg.Wrap = !m.cfg.Wrap
		if m.cfg.Wrap && m.view.Split() {
			// Wrapping needs the full width, so it takes the view back to the
			// stacked layout instead of silently doing nothing.
			m.view.SetSplit(false)
			m.cfg.SplitView = false
		}
		m.view.SetWrap(m.cfg.Wrap)
		m.message = "wrap " + onOff(m.cfg.Wrap)
		m.saveSetting()
		return m, setMessageCmd(m.message)
	case key.Matches(msg, m.keys.ToggleSmooth):
		m.cfg.SmoothScroll = !m.cfg.SmoothScroll
		m.view.SetSmooth(m.cfg.SmoothScroll)
		m.message = "smooth scroll " + onOff(m.cfg.SmoothScroll)
		m.saveSetting()
		return m, setMessageCmd(m.message)
	case key.Matches(msg, m.keys.AllChanges):
		return m.toggleAllChanges()
	}

	return m.updateFocus(msg)
}

// switchTab activates a top-level tab.
func (m model) switchTab(tab int) (tea.Model, tea.Cmd) {
	if m.files.Tab() == tab {
		return m, nil
	}
	m.files.SetTab(tab)
	m.focus = focusSidebar
	if !m.showLeft {
		m.focus = focusMain
	}
	m.setSize()

	if tab == filelist.TabCommits {
		if m.files.Total() == 0 {
			m.message = "loading commits…"
			return m, tea.Batch(loadLogCmd(m.cfg.LogLimit), setMessageCmd(m.message))
		}
		if c, ok := m.files.SelectedCommit(); ok {
			return m.showCommit(c)
		}
		return m, nil
	}

	// Back on the files tab: the pane may still be showing a commit diff, so
	// drop the commit notes and force the selected file to reload.
	m.view.SetNotes(nil)
	m.lastLoaded.path, m.lastLoaded.staged = "", false
	if m.files.Total() == 0 {
		m.allChanges = false
		m.allLoading = false
		m.view.SetFile(nil)
		m.d = nil
		return m, nil
	}
	if m.allChanges {
		return m.startAllChanges()
	}
	return m.autoLoadFile()
}

// cycleFocus moves focus across the panes of the active tab, skipping the
// sidebars while they are hidden.
func (m model) cycleFocus(back bool) (tea.Model, tea.Cmd) {
	order := make([]focus, 0, 3)
	if m.showLeft {
		order = append(order, focusSidebar)
	}
	order = append(order, focusMain)
	if m.files.Tab() == filelist.TabCommits && m.showRight && len(m.commitFileList) > 0 {
		order = append(order, focusCommitFiles)
	}
	cur := 0
	for i, f := range order {
		if f == m.focus {
			cur = i
			break
		}
	}
	if back {
		cur = (cur - 1 + len(order)) % len(order)
	} else {
		cur = (cur + 1) % len(order)
	}
	m.focus = order[cur]
	return m, nil
}

func (m model) updateFocus(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// shift+tab walks focus backwards through the same pane order as tab.
	if key.Matches(msg, m.keys.CycleBack) {
		return m.cycleFocus(true)
	}
	switch m.focus {
	case focusSidebar:
		switch {
		case key.Matches(msg, m.keys.Cycle):
			return m.cycleFocus(false)
		case key.Matches(msg, m.keys.Accept):
			// Directories expand/collapse; files open in the main pane.
			if m.files.ToggleSelectedDir() {
				return m, nil
			}
			m.focus = focusMain
			// Make sure the pane matches the row that was just accepted,
			// even when the selection changed without a key press (mouse
			// wheel, click, a fresh load).
			next, cmd := m.followSelection(nil)
			nm := next.(model)
			if m.files.Tab() == filelist.TabCommits {
				// Enter on a commit lands on the start of its diff.
				nm.view.JumpTo(0)
			}
			return nm, cmd
		case key.Matches(msg, m.keys.Commit):
			return m.startCompose()
		case key.Matches(msg, m.keys.Push):
			return m.startPush()
		case key.Matches(msg, m.keys.Stage):
			return m.toggleStage(true)
		case key.Matches(msg, m.keys.Unstage):
			return m.toggleStage(false)
		case key.Matches(msg, m.keys.Filter):
			// Delegated to the list below (it owns the filter binding).
		}
		return m.updateSidebar(msg)

	case focusMain:
		switch {
		case key.Matches(msg, m.keys.Back):
			// Esc belongs to the pane: it clears searches and filters, and
			// never steals focus.
			return m, nil
		case key.Matches(msg, m.keys.Cycle):
			return m.cycleFocus(false)
		case key.Matches(msg, m.keys.Top):
			return m, m.view.ScrollTo(0)
		case key.Matches(msg, m.keys.Bottom):
			return m, m.view.GotoBottom()
		case key.Matches(msg, m.keys.Search):
			cmd := m.search.Activate()
			m.setSize()
			return m, cmd
		case key.Matches(msg, m.keys.SearchNext):
			if m.view.MatchCount() > 0 {
				m.view.HighlightNext()
			}
			return m, nil
		case key.Matches(msg, m.keys.SearchPrev):
			if m.view.MatchCount() > 0 {
				m.view.HighlightPrevious()
			}
			return m, nil
		case key.Matches(msg, m.keys.YankFile):
			if patch := m.view.CurrentFilePatch(); patch != "" {
				m.message = "copied patch"
				return m, tea.Batch(tea.SetClipboard(patch), setMessageCmd(m.message))
			}
			m.message = "nothing to copy"
			return m, setMessageCmd(m.message)
		case key.Matches(msg, m.keys.YankHunk):
			if patch := m.view.CurrentHunkPatch(); patch != "" {
				return m, tea.SetClipboard(patch)
			}
			return m, nil
		case key.Matches(msg, m.keys.ToggleWS):
			m.cfg.IgnoreWhitespace = !m.cfg.IgnoreWhitespace
			m.message = "ignore whitespace " + onOff(m.cfg.IgnoreWhitespace)
			return m.reloadContent()
		case key.Matches(msg, m.keys.CycleCtx):
			m.cfg.Context = nextContext(m.cfg.Context)
			m.message = "context lines: " + itoa(m.cfg.Context)
			return m.reloadContent()
		case key.Matches(msg, m.keys.Commit):
			return m.startCompose()
		case key.Matches(msg, m.keys.Push):
			return m.startPush()
		}
		// Keys belong only to the main pane; the sidebar must not scroll.
		return m.forwardMain(msg)

	default: // focusCommitFiles
		switch {
		case key.Matches(msg, m.keys.Back):
			m.focus = focusMain
			return m, nil
		case key.Matches(msg, m.keys.Cycle):
			return m.cycleFocus(false)
		case key.Matches(msg, m.keys.Accept):
			if idx := m.files.CommitFilesIndex(); idx >= 0 {
				m.focus = focusMain
				m.view.JumpToBandNow(idx)
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.files, cmd = m.files.UpdateCommitFiles(msg)
		m.focus = focusCommitFiles
		if idx := m.files.CommitFilesIndex(); idx >= 0 {
			m.view.JumpToBandNow(idx)
		}
		return m, cmd
	}
}

// updateSidebar forwards keys to the sidebar list and follows selection
// changes into the main pane.
func (m model) updateSidebar(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.files, cmd = m.files.Update(msg)
	return m.followSelection(cmd)
}

// followSelection loads the diff for whatever the sidebar now points at.
// extra is folded into the returned command so callers can keep the list
// update command in the same batch.
func (m model) followSelection(extra tea.Cmd) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// In the all-changes view the pane already holds every diff: following a
	// selection just scrolls to that file's band.
	if m.allChanges && m.files.Tab() == filelist.TabFiles {
		if fs, _ := m.files.SelectedFile(); fs.Path != "" {
			m.view.JumpToBandNow(m.files.SelectedOrdinal() - 1)
		}
		return m, extra
	}

	switch m.files.Tab() {
	case filelist.TabFiles:
		fs, staged := m.files.SelectedFile()
		if fs.Path != "" && (fs.Path != m.lastLoaded.path || staged != m.lastLoaded.staged || m.lastLoaded.path == "") {
			next, loadCmd := m.showFile(fs.Path, staged)
			m = next.(model)
			var preCmd tea.Cmd
			m, preCmd = m.prefetchNeighbors()
			cmd = tea.Batch(loadCmd, preCmd)
		}
	case filelist.TabCommits:
		if c, ok := m.files.SelectedCommit(); ok && c.Hash != m.commit.Hash {
			next, loadCmd := m.showCommit(c)
			m = next.(model)
			cmd = loadCmd
		}
	}

	return m, tea.Batch(extra, cmd)
}

// stepFile moves the selection by delta files and follows it into the main
// pane. It powers the J/K next/previous file bindings.
func (m model) stepFile(delta int) (tea.Model, tea.Cmd) {
	if m.files.Tab() == filelist.TabCommits {
		if m.files.Total() == 0 {
			return m, nil
		}
		m.files.MoveSelection(delta)
		return m.followSelection(nil)
	}
	if !m.files.MoveSelectable(delta) {
		return m, nil
	}
	return m.followSelection(nil)
}

// toggleAllChanges switches the files tab between one-file-at-a-time and a
// single document holding every working-tree change.
func (m model) toggleAllChanges() (tea.Model, tea.Cmd) {
	if m.files.Tab() != filelist.TabFiles {
		m.message = "all changes is only available on the files tab"
		return m, setMessageCmd(m.message)
	}

	m.allChanges = !m.allChanges
	if !m.allChanges {
		m.allLoading = false
		m.allPendingJump = false
		m.allAdds, m.allDels = 0, 0
		m.message = "single file view"
		next, cmd := m.followSelection(nil)
		return next, tea.Batch(cmd, setMessageCmd(m.message))
	}

	if m.files.Total() == 0 {
		m.allChanges = false
		m.message = "nothing to show"
		return m, setMessageCmd(m.message)
	}

	m.allLoading = true
	m.lastLoaded.path, m.lastLoaded.staged = "", false
	m.view.SetFile(nil)
	m.view.SetNotes(nil)
	m.d = nil
	m.message = "all changes"
	next, cmd := m.startAllChanges()
	return next, tea.Batch(cmd, setMessageCmd(m.message))
}

// startAllChanges restarts the streaming all-changes document. The generation
// token makes chunks from an older load harmless: they arrive, they are
// dropped.
func (m model) startAllChanges() (tea.Model, tea.Cmd) {
	entries := m.files.OrderedEntries()
	opts := m.diffOptions()

	m.allGen++
	m.allLoading = true
	m.allPendingJump = false
	m.allAdds, m.allDels = 0, 0
	// A refresh keeps the reader in place; a toggle starts at the file the
	// sidebar selects, which is what the reader just asked for.
	m.allRestore, m.allAnchor = m.allAnchor, -1
	m.view.SetNotes(nil)
	m.view.SetDocument(blockLoader(entries, opts))
	m.d = &diff.Diff{}

	return m, loadAllChangesCmd(entries, m.allGen, opts)
}

// blockLoader re-diffs block i when the virtual document needs its rows
// again. The status snapshot and the content-addressed diff cache make this a
// map lookup in the common case.
func blockLoader(entries []filelist.Entry, opts git.Options) func(i int) *diff.File {
	return func(i int) *diff.File {
		if i < 0 || i >= len(entries) {
			return nil
		}
		e := entries[i]
		f, err := git.FileDiff(e.Status.Path, e.Staged, opts)
		if err != nil || f == nil {
			return &diff.File{
				OldPath: e.Status.Path,
				NewPath: e.Status.Path,
				Kind:    diff.Modified,
				Lines:   []diff.Line{{Kind: diff.NoNewline, Text: "diff unavailable"}},
			}
		}
		return f
	}
}

func (m model) toggleStage(staged bool) (tea.Model, tea.Cmd) {
	fs, isStaged := m.files.SelectedFile()
	if fs.Path == "" {
		return m, nil
	}
	// "s" stages (from the unstaged section), "u" unstages.
	if staged == isStaged {
		return m, nil
	}
	return m, stageCmd(fs.Path, staged)
}

func (m model) startCompose() (tea.Model, tea.Cmd) {
	if m.files.StagedCount() == 0 {
		m.message = "nothing staged — press s to stage files"
		return m, setMessageCmd(m.message)
	}
	m.state = stateCompose
	m.focus = focusSidebar
	m.compose.Reset()
	m.setSize()
	return m, m.compose.Focus()
}

func (m model) updateCompose(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = stateShowList
		m.compose.Blur()
		m.setSize()
		return m, nil
	case "ctrl+s", "ctrl+enter":
		text := m.compose.Value()
		if text == "" {
			m.message = "commit message is empty"
			return m, setMessageCmd(m.message)
		}
		return m, commitCmd(text)
	case "ctrl+c":
		return m, tea.Quit
	}
	return m.updateChildren(msg)
}

func (m model) updateSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Results are already live; enter just leaves the input and keeps the
		// query on screen, muted, until esc clears it.
		m.search.Deactivate()
		m.searchApplied = m.view.SearchTerm() != ""
		m.setSize()
		return m, nil
	case "esc":
		m.search.Deactivate()
		m.search.Clear()
		m.searchApplied = false
		m.view.ClearSearch()
		m.setSize()
		return m, nil
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	cmds = append(cmds, cmd)
	m.spinner, cmd = m.spinner.Update(msg)
	cmds = append(cmds, cmd)

	// Search as the term is typed: the window answers now, the rest of the
	// document follows from the background scan.
	if term := m.search.Value(); term == "" {
		m.view.ClearSearch()
	} else {
		_, searchCmd := m.view.Search(term)
		if searchCmd != nil {
			cmds = append(cmds, searchCmd)
		}
	}
	return m, tea.Batch(cmds...)
}

// forwardMain forwards keys to the diff pane only, so the sidebar never
// scrolls while the main content has focus.
func (m model) forwardMain(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.view, cmd = m.view.Update(msg)
	cmds = append(cmds, cmd)
	m.spinner, cmd = m.spinner.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

// refresh reloads the data behind the current page.
func (m model) refresh() (tea.Model, tea.Cmd) {
	switch m.files.Tab() {
	case filelist.TabCommits:
		return m, loadLogCmd(m.cfg.LogLimit)
	default:
		return m, m.requestStatus(false)
	}
}

// reloadContent regenerates whatever diff the main pane is showing. The
// cached copy for the old options is dropped because the options changed.
func (m model) reloadContent() (tea.Model, tea.Cmd) {
	if m.files.Tab() == filelist.TabCommits {
		m.cache.Delete(commitCacheKey(m.commit.Hash, m.cfg))
		next, cmd := m.showCommit(m.commit)
		return next, tea.Batch(cmd, setMessageCmd(m.message))
	}
	if m.allChanges {
		next, cmd := m.startAllChanges()
		return next, tea.Batch(cmd, setMessageCmd(m.message))
	}
	fs, staged := m.files.SelectedFile()
	if fs.Path == "" {
		return m, tea.Batch(m.requestStatus(false), setMessageCmd(m.message))
	}
	m.cache.Delete(fileCacheKey(fs.Path, staged, m.cfg))
	next, cmd := m.showFile(fs.Path, staged)
	return next, tea.Batch(cmd, setMessageCmd(m.message))
}

func (m *model) applyStyles(st styles.Styles) {
	m.styles = st
	m.files.SetStyles(st)
	m.view.SetStyles(st)
	m.status.SetStyles(st)
	m.search.SetStyles(st)
	m.compose.SetStyles(st)
	m.spinner = spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(st.SpinnerStyle()))
}

// setSize computes the persistent chrome and the pane rectangles with the
// ultraviolet layout engine. The help view lives below the footer bar, like
// glow, so it eats into the content height while open.
func (m *model) setSize() {
	w, h := m.width, m.height
	if w < 1 || h < 1 {
		return
	}

	helpH := 0
	if m.help {
		helpH = m.helpHeight()
	}
	contentH := max(1, h-2-helpH) // header bar + footer bar

	var header, body, footer uv.Rectangle
	layout.Vertical(layout.Len(1), layout.Len(contentH), layout.Len(1)).
		Split(uv.Rect(0, 0, w, h)).
		Assign(&header, &body, &footer)

	sidebarW := min(36, max(16, w/3))
	filesW := min(30, max(14, w/4))
	if !m.showLeft {
		sidebarW = 0
	}
	if !m.showRight {
		filesW = 0
	}

	// The search line (input or applied) takes a row of the main pane, so the
	// diff renders one row shorter and its scrollbar ends up right above it.
	searchRows := 0
	if m.search.Active() || m.searchApplied {
		searchRows = 1
	}

	switch m.files.Tab() {
	case filelist.TabCommits:
		var commits, main, commitFiles uv.Rectangle
		layout.Horizontal(layout.Len(sidebarW), layout.Fill(1), layout.Len(filesW)).
			Split(body).
			Assign(&commits, &main, &commitFiles)
		m.sideRect = commits
		m.mainRect = main
		m.commitFilesRct = commitFiles
		m.files.SetSize(max(1, max(0, sidebarW-2)), max(1, commits.Dy()-2))
		m.files.SetCommitFilesSize(max(1, max(0, filesW-2)), max(1, commitFiles.Dy()-2))
		m.view.SetSize(max(1, main.Dx()-2), max(1, main.Dy()-2-searchRows))
	default:
		var side, main uv.Rectangle
		layout.Horizontal(layout.Len(sidebarW), layout.Fill(1)).
			Split(body).
			Assign(&side, &main)
		m.sideRect = side
		m.mainRect = main
		m.commitFilesRct = uv.Rect(0, 0, 0, 0)
		m.files.SetSize(max(1, max(0, side.Dx()-2)), max(1, side.Dy()-2))
		m.view.SetSize(max(1, main.Dx()-2), max(1, main.Dy()-2-searchRows))
	}

	m.status.SetWidth(w)
	m.search.SetWidth(max(1, m.mainRect.Dx()-2))
}

func nextContext(current int) int {
	for i, c := range contextCycle {
		if c == current {
			return contextCycle[(i+1)%len(contextCycle)]
		}
	}
	return 3
}

func viewMode(split bool) string {
	if split {
		return "split"
	}
	return "unified"
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// commitNotes renders the commit metadata block shown above a commit diff.
func commitNotes(st styles.Styles, c git.Commit, files []*diff.File) []string {
	// No left margin: the pane's own gutter already separates text from the
	// border, and a leading space shows up as an indent when wrap is off.
	hash := lipgloss.NewStyle().Foreground(st.HashActiveFg()).Bold(true).Render(c.Hash)
	meta := st.Subtle().Render(c.Author + " · " + c.When)
	title := lipgloss.NewStyle().Foreground(st.TitleActiveFg()).Bold(true).Render(c.Subject)
	lines := []string{
		hash + " " + meta,
		title,
	}
	// The commit body belongs to the diff pane, under the subject, and is
	// shown in full: every line the message has, wrapped at the pane width.
	body := lipgloss.NewStyle().Foreground(st.DescNormalFg())
	if desc := strings.TrimSpace(c.Description); desc != "" {
		for _, line := range strings.Split(desc, "\n") {
			lines = append(lines, body.Render(strings.TrimRight(line, " \t")))
		}
	}
	lines = append(lines, changedFileNotes(st, files)...)
	return append(lines, "")
}

// commitFileNoteLimit caps the file summary in the notes: a commit with a
// thousand files already has its per-file bands in the diff below.
const commitFileNoteLimit = 20

// changedFileNotes renders the commit's changed files with their line counts,
// so the pane says what changed before the first hunk is scrolled into view.
// Paths form one column and the two count columns line up to their right; the
// header carries the totals in the same columns. Counts that are not known yet
// (a lazily loaded commit) leave their columns off.
func changedFileNotes(st styles.Styles, files []*diff.File) []string {
	if len(files) == 0 {
		return nil
	}
	shown := min(len(files), commitFileNoteLimit)

	maxPath, maxAdd, maxDel := 0, 0, 0
	totalAdd, totalDel, known := 0, 0, 0
	for _, f := range files {
		maxPath = max(maxPath, ansi.StringWidth(f.Path()))
		if f.Additions >= 0 && f.Deletions >= 0 {
			totalAdd += f.Additions
			totalDel += f.Deletions
			known++
		}
	}
	for _, f := range files[:shown] {
		if f.Additions >= 0 && f.Deletions >= 0 {
			maxAdd = max(maxAdd, len("+"+itoa(f.Additions)))
			maxDel = max(maxDel, len("-"+itoa(f.Deletions)))
		}
	}
	if known > 0 {
		maxAdd = max(maxAdd, len("+"+itoa(totalAdd)))
		maxDel = max(maxDel, len("-"+itoa(totalDel)))
	}
	// The header shares the path column, so it counts towards its width.
	maxPath = max(maxPath, ansi.StringWidth(plural(len(files), "file")+" changed"))

	// row pads the label into the path column, then right-aligns the counts in
	// their own columns so every line reads as a table.
	row := func(plain, styled string, add, del int, counts bool) string {
		if !counts {
			return styled
		}
		gap := strings.Repeat(" ", max(1, maxPath-ansi.StringWidth(plain)+4))
		a, d := "+"+itoa(add), "-"+itoa(del)
		return styled + gap +
			strings.Repeat(" ", maxAdd-len(a)) + st.GreenFg(a) + "  " +
			strings.Repeat(" ", maxDel-len(d)) + st.RedFg(d)
	}

	header := plural(len(files), "file") + " changed"
	lines := []string{"", row(header, st.Subtle().Render(" "+header), totalAdd, totalDel, known > 0)}
	pathStyle := lipgloss.NewStyle().Foreground(st.DescNormalFg())
	for _, f := range files[:shown] {
		counts := f.Additions >= 0 && f.Deletions >= 0
		lines = append(lines, row(f.Path(), " "+pathStyle.Render(f.Path()), f.Additions, f.Deletions, counts))
	}
	if rest := len(files) - shown; rest > 0 {
		lines = append(lines, st.Subtle().Render(" … "+itoa(rest)+" more"))
	}
	return lines
}

func loadStatusCmd(polled bool) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		st, err := git.WorkingTree()
		return statusLoadedMsg{
			staged:      st.Staged,
			unstaged:    st.Unstaged,
			fingerprint: st.Fingerprint,
			cost:        time.Since(start),
			polled:      polled,
			err:         err,
		}
	}
}

// loadRepoInfoCmd resolves the header label: the branch and the repository name.
// It opens the repository, so it runs as a command rather than in front of the
// first frame.
func loadRepoInfoCmd() tea.Cmd {
	return func() tea.Msg {
		return repoInfoMsg{branch: git.Branch(), project: git.ProjectName()}
	}
}

func loadFileDiffCmd(path string, staged bool, opts git.Options, seq int) tea.Cmd {
	return func() tea.Msg {
		f, err := git.FileDiff(path, staged, opts)
		return fileLoadedMsg{file: f, err: err, path: path, staged: staged, seq: seq}
	}
}

func loadCommitDiffCmd(c git.Commit, opts git.Options, seq int) tea.Cmd {
	return func() tea.Msg {
		d, err := git.Diff(git.Source{Kind: git.SourceCommit, Ref1: c.Hash}, opts)
		if err != nil {
			return commitLoadedMsg{commit: c, err: err, seq: seq}
		}
		return commitLoadedMsg{file: diff.Aggregate(d.Files), files: d.Files, commit: c, seq: seq}
	}
}

// loadCommitListCmd asks for a commit's changed paths, which comes from tree
// data alone and works even when the content is not present locally.
func loadCommitListCmd(c git.Commit, seq int) tea.Cmd {
	return func() tea.Msg {
		changes, err := git.CommitChangeList(c.Hash)
		return commitListMsg{commit: c, changes: changes, seq: seq, err: err}
	}
}

// loadCommitChunkCmd diffs the next slice of a large commit's files.
func loadCommitChunkCmd(c git.Commit, changes []git.CommitChange, from, seq int, opts git.Options) tea.Cmd {
	return func() tea.Msg {
		end := min(from+commitChunkFiles, len(changes))
		files := make([]*diff.File, 0, max(0, end-from))
		for _, ch := range changes[from:end] {
			f, err := git.FileAtCommit(c.Hash, ch, opts)
			if err != nil {
				f = diff.Unavailable(ch.Path, err.Error())
			}
			files = append(files, f)
		}
		return commitChunkMsg{commit: c, changes: changes, files: files, from: from, seq: seq}
	}
}

func loadLogCmd(limit int) tea.Cmd {
	return func() tea.Msg {
		commits, err := git.Log(limit)
		return logLoadedMsg{commits: commits, err: err}
	}
}

// allChunkSize is how many files the all-changes loader diffs per message.
// Small enough that the first paint is immediate, large enough that the
// per-message overhead disappears.
const allChunkSize = 24

const (
	// commitEagerFiles is how many changed files a commit may have before its
	// document is built lazily. Below it the per-file stats are worth diffing
	// everything up front; above it, generating every file before the first
	// screen would make a large merge commit expensive to even look at.
	commitEagerFiles = 64

	// commitChunkFiles is how many of a large commit's files are diffed per
	// message while its document streams in.
	commitChunkFiles = 12
)

// loadAllChangesCmd starts the all-changes document. Files are diffed a chunk
// at a time and appended to the document as they arrive, so a repository with
// thousands of changed files still shows its first screen immediately.
func loadAllChangesCmd(entries []filelist.Entry, gen int, opts git.Options) tea.Cmd {
	return loadAllChunkCmd(entries, 0, gen, opts)
}

func loadAllChunkCmd(entries []filelist.Entry, from, gen int, opts git.Options) tea.Cmd {
	return func() tea.Msg {
		end := min(from+allChunkSize, len(entries))
		targets := make([]git.DiffTarget, end-from)
		for i, e := range entries[from:end] {
			targets[i] = git.DiffTarget{Path: e.Status.Path, Staged: e.Staged}
		}
		files, err := git.DiffTargets(targets, opts)
		return allChunkMsg{
			entries: entries,
			files:   files,
			from:    from,
			gen:     gen,
			done:    end >= len(entries),
			err:     err,
		}
	}
}

func stageCmd(path string, staged bool) tea.Cmd {
	return func() tea.Msg {
		var err error
		if staged {
			err = git.Stage(path)
		} else {
			err = git.Unstage(path)
		}
		return stageDoneMsg{staged: staged, err: err}
	}
}

func commitCmd(message string) tea.Cmd {
	return func() tea.Msg {
		hash, err := git.CommitChanges(message)
		return committedMsg{hash: hash, err: err}
	}
}

// startPush runs the push in the background and marks the footer busy. A push
// goes over the network, so it can take a while, and a silent pause would read
// as a hang.
func (m model) startPush() (tea.Model, tea.Cmd) {
	if m.pushing {
		return m, nil
	}
	m.pushing = true
	m.message = ""
	return m, pushCmd()
}

func pushCmd() tea.Cmd {
	return func() tea.Msg {
		return pushedMsg{err: git.Push()}
	}
}

func setMessageCmd(string) tea.Cmd {
	return tea.Tick(statusMessageTimeout, func(time.Time) tea.Msg {
		return clearMsgMsg{}
	})
}

// ---------------------------------------------------------------- config file

// configWatchInterval is how often the config file is re-checked. A tick is
// cheaper than a filesystem watcher dependency, and a second of latency on an
// edit made in another window is not worth one.
const configWatchInterval = time.Second

// configTickMsg asks the model to look at the config file again.
type configTickMsg struct{}

func configTick() tea.Cmd {
	return tea.Tick(configWatchInterval, func(time.Time) tea.Msg {
		return configTickMsg{}
	})
}

// configStamp fingerprints the config file by mtime and size. An empty stamp
// means the file does not exist yet.
func configStamp(path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
}

// applyConfig makes a freshly loaded config take effect without a restart.
func (m *model) applyConfig(cfg config.Config) tea.Cmd {
	old := m.cfg
	m.cfg = cfg
	m.view.SetSmooth(cfg.SmoothScroll)
	m.view.SetLineNumbers(cfg.LineNumbers)
	// Wrap cannot ride on top of the two-column layout: the columns are
	// already narrow, and wrapped columns would need variable row heights.
	cfg.SplitView = cfg.SplitView && !cfg.Wrap
	m.view.SetWrap(cfg.Wrap)
	m.view.SetSplit(cfg.SplitView)
	// The sidebars are also toggled by hand, so they only move when the file
	// on disk actually says something different from what is on screen.
	if cfg.ShowLeftSidebar != old.ShowLeftSidebar {
		m.showLeft = cfg.ShowLeftSidebar
		if !m.showLeft && m.focus == focusSidebar {
			m.focus = focusMain
		}
	}
	if cfg.ShowRightSidebar != old.ShowRightSidebar {
		m.showRight = cfg.ShowRightSidebar
		if !m.showRight && m.focus == focusCommitFiles {
			m.focus = focusMain
		}
	}
	if cfg.SidebarWidth != old.SidebarWidth || cfg.LogLimit != old.LogLimit ||
		cfg.ShowLeftSidebar != old.ShowLeftSidebar || cfg.ShowRightSidebar != old.ShowRightSidebar {
		m.setSize()
	}
	if cfg.Context != old.Context || cfg.IgnoreWhitespace != old.IgnoreWhitespace {
		next, cmd := m.reloadContent()
		*m = next.(model)
		return cmd
	}
	if cfg.Watch != old.Watch {
		if cfg.Watch {
			// A watcher that failed before is worth retrying: the reason may
			// have been a limit that was raised since.
			m.watchOff = false
			dirs, files := m.watchTargets()
			return tea.Batch(m.startWatch(dirs, files), m.statusTick())
		}
		m.stopWatch()
		return nil
	}
	return nil
}

// saveSetting persists one TUI toggle, appending the failure to the current
// message instead of dropping it. The file is always written whole, so it
// stays a complete configuration whatever was toggled.
func (m *model) saveSetting() {
	if err := m.saveConfig(); err != nil {
		m.message += " (not saved: " + err.Error() + ")"
	}
}

// saveConfig writes the current settings back to the config file, so a toggle
// survives the next start. The stamp is refreshed so the watcher does not read
// our own write as an external edit.
func (m *model) saveConfig() error {
	if m.cfgPath == "" {
		return nil
	}
	if err := config.Save(m.cfgPath, m.cfg); err != nil {
		return err
	}
	m.cfgStamp = configStamp(m.cfgPath)
	return nil
}

// watchConfig reloads the config file when it changed on disk, so an edit made
// in another window shows up in the running TUI. Twig's own saves refresh the
// stamp first, so they never trigger a reload.
func (m model) watchConfig() (tea.Model, tea.Cmd) {
	if m.cfgPath == "" {
		m.configTicking = false
		return m, nil
	}

	stamp := configStamp(m.cfgPath)
	if stamp == m.cfgStamp {
		return m, configTick()
	}
	m.cfgStamp = stamp
	if stamp == "" {
		// The file was removed: keep the settings that are running.
		return m, configTick()
	}

	cfg, err := config.Load(m.cfgPath)
	if err != nil {
		m.message = "config: " + err.Error()
		return m, tea.Batch(configTick(), setMessageCmd(m.message))
	}

	cmd := m.applyConfig(cfg)
	m.message = "config reloaded"
	return m, tea.Batch(cmd, configTick(), setMessageCmd(m.message))
}
