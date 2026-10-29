package ui

import (
	"twig/internal/diff"
	"twig/internal/git"
	"twig/internal/ui/components/filelist"
)

// statusLoadedMsg carries the working tree status for the Files page.
type statusLoadedMsg struct {
	staged   []git.FileStatus
	unstaged []git.FileStatus
	err      error
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

// stageDoneMsg reports the result of a stage or unstage operation.
type stageDoneMsg struct {
	staged bool
	err    error
}

// committedMsg reports the result of creating a commit.
type committedMsg struct {
	hash string
	err  error
}

// pushedMsg reports the result of a push.
type pushedMsg struct {
	err error
}

// clearMsgMsg clears the transient status message.
type clearMsgMsg struct{}

// prefetchedMsg carries a background-loaded file diff for the cache.
type prefetchedMsg struct {
	key  string
	file *diff.File
	err  error
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
