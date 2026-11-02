package ui

import (
	"time"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/ui/components/diffview"
	"samouly.fun/twig/internal/ui/components/filelist"
)

// statusLoadedMsg carries the working tree status for the Files page.
type statusLoadedMsg struct {
	staged   []git.FileStatus
	unstaged []git.FileStatus
	// fingerprint identifies the state this status came from, so a refresh
	// that found nothing new does not repaint.
	fingerprint string
	// cost is how long the status took, which sets the poll interval.
	cost time.Duration
	// polled records that the periodic pass asked for this run rather than a
	// filesystem event, which is how a watcher that stopped delivering is
	// caught.
	polled bool
	err    error
}

// fileLoadedMsg carries the diff of a single file for the pager.
type fileLoadedMsg struct {
	file   *diff.File
	path   string
	staged bool
	seq    int
	err    error
}

// commitLoadedMsg carries the diff and metadata of a commit for the pager.
type commitLoadedMsg struct {
	file   *diff.File
	files  []*diff.File
	commit git.Commit
	seq    int
	err    error
}

// repoInfoMsg carries the repository label. Resolving it opens the repository
// and decodes HEAD, which takes seconds on a large one, so it arrives after the
// first frame instead of in front of it.
type repoInfoMsg struct {
	branch  string
	project string
}

// commitListMsg carries a commit's changed paths, which is tree data only:
// listing even a merge commit reads no blobs.
type commitListMsg struct {
	commit  git.Commit
	changes []git.CommitChange
	seq     int
	err     error
}

// commitChunkMsg carries the next slice of a large commit's files, already
// diffed, so the document grows while the rest of the commit is still unloaded.
type commitChunkMsg struct {
	commit  git.Commit
	changes []git.CommitChange
	files   []*diff.File
	from    int
	seq     int
	err     error
}

// logLoadedMsg carries the result of loading the commit log.
type logLoadedMsg struct {
	commits []git.Commit
	err     error
}

// stageDoneMsg reports the result of a staging operation: staging, unstaging
// or discarding, at whatever scope the cursor named.
type stageDoneMsg struct {
	staged bool
	// op is "stage", "unstage" or "discard".
	op string
	// path is the file the operation touched.
	path string
	// scope is what the cursor covered: "file", "hunk", "line" or "range".
	scope string
	// lines is how many changed lines moved.
	lines int
	// undo reverses the operation exactly.
	undo git.Undo
	// anchor is where the reader was, so the reload can put them back.
	anchor diffview.CursorAnchor
	err    error
}

// committedMsg reports the result of creating a commit.
type committedMsg struct {
	hash string
	err  error
}

// undoDoneMsg reports the result of reversing an operation.
type undoDoneMsg struct {
	label string
	err   error
}

// pushedMsg reports the result of a push.
type pushedMsg struct {
	err error
}

// clearMsgMsg clears the transient status message.
type clearMsgMsg struct{}

// prefetchedMsg carries a background-loaded file diff for the cache. The
// fingerprint says which working-tree state it was taken from, so a prefetch that
// lands after the tree moved is dropped instead of caching a stale diff.
type prefetchedMsg struct {
	key         string
	file        *diff.File
	err         error
	fingerprint string
}

// allLoadedMsg carries the aggregate of every working-tree change used by the
// all-changes view.
type allLoadedMsg struct {
	file *diff.File
	err  error
}

// allChunkMsg carries the next slice of the all-changes document while it is
// still being assembled, so the first files appear long before the last ones
// have been diffed.
type allChunkMsg struct {
	entries []filelist.Entry
	files   []*diff.File
	from    int
	gen     int
	done    bool
	err     error
}
