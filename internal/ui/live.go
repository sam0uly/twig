package ui

import (
	"os"

	tea "charm.land/bubbletea/v2"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/ui/components/diffview"
	"samouly.fun/twig/internal/ui/components/filelist"
)

// liveKey names one changed line by the side it is shown from and its text. The line
// number is absent on purpose: a line that only moved has not been edited.
type liveKey struct {
	pre  bool
	text string
	// n is which occurrence of that text it is, so two identical added lines are two
	// identities.
	n int
}

// liveKeyOf returns the identity of a change row, or false for a context line.
func liveKeyOf(l diff.Line) (liveKey, bool) {
	switch l.Kind {
	case diff.Addition:
		return liveKey{text: l.Text}, true
	case diff.Deletion:
		return liveKey{pre: true, text: l.Text}, true
	}
	return liveKey{}, false
}

// liveChanges is every changed line of a diff, keyed by identity. The occurrence
// index is counted in file order, so both sides produce the same keys.
func liveChanges(f *diff.File) map[liveKey]struct{} {
	out := make(map[liveKey]struct{}, 16)
	if f == nil {
		return out
	}
	seen := make(map[liveKey]int, 16)
	for i := range f.Hunks {
		h, ok := f.HunkAt(i)
		if !ok {
			continue
		}
		for _, l := range h.Lines {
			key, ok := liveKeyOf(l)
			if !ok {
				continue
			}
			key.n = seen[key]
			seen[liveKey{pre: key.pre, text: key.text}]++
			out[key] = struct{}{}
		}
	}
	return out
}

// followedChange is the last line in next that prev did not have: a tool that
// rewrites a file in several places leaves the reader where it stopped. An addition
// is preferred over the deletion it replaced.
func followedChange(prev, next *diff.File) (diff.Line, bool) {
	if next == nil {
		return diff.Line{}, false
	}
	before := liveChanges(prev)
	var last, deletion diff.Line
	haveLast, haveDeletion := false, false
	seen := make(map[liveKey]int, 16)
	for i := range next.Hunks {
		h, ok := next.HunkAt(i)
		if !ok {
			continue
		}
		for _, l := range h.Lines {
			key, ok := liveKeyOf(l)
			if !ok {
				continue
			}
			at := liveKey{pre: key.pre, text: key.text}
			key.n = seen[at]
			seen[at]++
			if _, found := before[key]; found {
				continue
			}
			last, haveLast = l, true
			if key.pre && !haveDeletion {
				deletion, haveDeletion = l, true
			}
		}
	}
	if haveLast {
		return last, true
	}
	return deletion, haveDeletion
}

// firstChange is the first changed line of a diff, where a file nobody has seen is
// opened.
func firstChange(f *diff.File) (diff.Line, bool) {
	if f == nil {
		return diff.Line{}, false
	}
	for i := range f.Hunks {
		h, ok := f.HunkAt(i)
		if !ok {
			continue
		}
		var deletion diff.Line
		haveDeletion := false
		for _, l := range h.Lines {
			switch l.Kind {
			case diff.Addition:
				return l, true
			case diff.Deletion:
				if !haveDeletion {
					deletion, haveDeletion = l, true
				}
			}
		}
		if haveDeletion {
			return deletion, true
		}
	}
	return diff.Line{}, false
}

// livePath names one diff of a file, which the listing holds twice when it is
// partly staged.
type livePath struct {
	path   string
	staged bool
}

// liveFile is what one side of a path looked like when it was last seen: its stat,
// which says it changed without opening it, and the diff, which says which line.
// Every listed row carries a stat; only the ones on screen carry a diff.
type liveFile struct {
	size  int64
	mtime int64
	diff  *diff.File
}

// maxLiveRows is the largest diff kept as a follow baseline. A file someone is
// typing into is small, and a rewrite of a million lines is not a line anyone
// chases: holding its rows costs more than the follow is worth.
const maxLiveRows = 8000

// maxLiveDiffs bounds how many baselines are held at once, so a burst that touches
// several files cannot grow the model by everything that moved.
const maxLiveDiffs = 8

// rememberDiff records the diff one side of a path has now, so the next change can
// be named. The stat is left alone: it is the baseline the next change is measured
// against.
func (m *model) rememberDiff(path string, f *diff.File, staged bool) {
	if path == "" || f == nil || !m.cfg.Follow {
		return
	}
	if m.liveNews == nil {
		m.liveNews = make(map[livePath]*liveFile)
	}
	key := livePath{path: path, staged: staged}
	entry := m.liveNews[key]
	if entry == nil {
		size, mtime := statKey(path)
		entry = &liveFile{size: size, mtime: mtime}
		m.liveNews[key] = entry
	}
	if f.Additions+f.Deletions > maxLiveRows {
		m.dropDiff(key)
		return
	}
	if entry.diff == nil && m.heldDiffs() >= maxLiveDiffs {
		m.dropSomeDiff(key)
	}
	entry.diff = f
}

// dropDiff forgets one side's baseline but keeps its stat.
func (m *model) dropDiff(key livePath) {
	if entry := m.liveNews[key]; entry != nil {
		entry.diff = nil
	}
}

// dropSomeDiff forgets a baseline other than keep, so the newest one still fits.
func (m *model) dropSomeDiff(keep livePath) {
	for key, entry := range m.liveNews {
		if key == keep || entry.diff == nil {
			continue
		}
		entry.diff = nil
		return
	}
}

// heldDiffs counts the baselines the model holds.
func (m model) heldDiffs() int {
	n := 0
	for _, entry := range m.liveNews {
		if entry.diff != nil {
			n++
		}
	}
	return n
}

// statKey is the cheap evidence that a file changed: its size and mtime.
func statKey(path string) (size, mtime int64) {
	info, err := os.Stat(git.WorktreePath(path))
	if err != nil {
		return -1, -1
	}
	return info.Size(), info.ModTime().UnixNano()
}

// changedFiles reports the rows whose bytes moved since the last status load. Only
// the worktree side can move a stat, and a row that enters the listing is new work.
// The first load only records.
func (m *model) changedFiles(entries []filelist.Entry) []filelist.Entry {
	if m.liveNews == nil {
		m.liveNews = make(map[livePath]*liveFile)
	}
	// A staging reload keeps its place and a dialog is in front of the pane: leave the
	// baselines alone, so a change that arrived meanwhile is still detected.
	if m.anchorWait || m.state != stateShowList {
		return nil
	}
	known := len(m.liveNews) > 0
	var changed []filelist.Entry
	seen := make(map[livePath]struct{}, len(entries))
	reported := make(map[string]bool, len(entries))
	for _, e := range entries {
		path := e.Status.Path
		key := livePath{path: path, staged: e.Staged}
		seen[key] = struct{}{}
		size, mtime := entryStat(e)
		entry := m.liveNews[key]
		if entry == nil {
			// A row that was not listed before is new work: its first change is the change.
			m.liveNews[key] = &liveFile{size: size, mtime: mtime}
			if known && !e.Staged && !reported[path] {
				changed = append(changed, e)
				reported[path] = true
			}
			continue
		}
		moved := entry.size != size || entry.mtime != mtime
		entry.size, entry.mtime = size, mtime
		// A staged row is fingerprinted from the same file, so its numbers move with any
		// write: only the worktree row can be said to have changed.
		if known && !e.Staged && !reported[path] && moved {
			changed = append(changed, e)
			reported[path] = true
		}
	}
	// A row that left the listing is not followed: forget its diff.
	for key := range m.liveNews {
		if _, ok := seen[key]; !ok {
			delete(m.liveNews, key)
		}
	}
	return changed
}

// liveFollow picks the change to chase.
func (m *model) liveFollow(changed []filelist.Entry) (filelist.Entry, bool) {
	if !m.cfg.Follow || !m.cfg.Watch || m.anchorWait || m.state != stateShowList || len(changed) == 0 {
		return filelist.Entry{}, false
	}
	// A burst can touch several files: the one written last is where the tool is. The
	// file on screen only wins a tie.
	newest := changed[0]
	_, best := entryStat(newest)
	for _, e := range changed[1:] {
		if _, at := entryStat(e); at > best {
			newest, best = e, at
		}
	}
	for _, e := range changed {
		if e.Status.Path != m.lastLoaded.path {
			continue
		}
		if _, at := entryStat(e); at >= best {
			return e, true
		}
	}
	return newest, true
}

// followLine names the line to chase in a diff that just arrived.
func (m *model) followLine(path string, staged bool, next *diff.File) (diff.Line, bool) {
	var prev *diff.File
	if entry := m.liveNews[livePath{path: path, staged: staged}]; entry != nil {
		prev = entry.diff
	}
	if prev != nil {
		return followedChange(prev, next)
	}
	return firstChange(next)
}

// followShown moves the pane to a line that just arrived on screen, and reports
// whether it went anywhere.
func (m *model) followShown(path string, staged bool, next *diff.File) bool {
	if !m.cfg.Follow || !m.cfg.Watch || m.anchorWait || m.state != stateShowList {
		return false
	}
	line, ok := m.followLine(path, staged, next)
	if !ok {
		return false
	}
	if m.view.FollowEdit(line) {
		m.dropAnchor()
		return true
	}
	return false
}

// followBlock is the all-changes version: the same line, in that diff's block.
func (m *model) followBlock(path string, staged bool, line diff.Line) {
	if !m.cfg.Follow || !m.cfg.Watch || m.anchorWait {
		return
	}
	if m.view.FollowEditInBand(m.bandOf(path, staged), path, line) {
		m.dropAnchor()
	}
}

// clearAllFollow forgets the change a rebuilt document was meant to open on. The side
// goes with the path, or the other half could claim the next follow.
func (m *model) clearAllFollow() {
	m.allFollow, m.allFollowStaged, m.allFollowOK = "", false, false
	m.allFollowLine = diff.Line{}
}

// bandOf is the block one side of a path occupies, or -1 when it is not listed.
func (m model) bandOf(path string, staged bool) int {
	for i, e := range m.files.OrderedEntries() {
		if e.Status.Path == path && e.Staged == staged {
			return i
		}
	}
	return -1
}

// blockUpdateMsg carries fresh diffs for blocks the all-changes document already
// holds, so a live change patches one file instead of rebuilding the view.
type blockUpdateMsg struct {
	updates []diffview.BlockUpdate
	// remembers are the diffs to keep as the new baseline, one per block.
	remembers []liveSide
	// follow is the line to chase, named against the diff it replaces.
	follow diff.Line
	path   string
	staged bool
	ok     bool
}

// reloadBlocksCmd loads fresh diffs for the blocks a live change touched. The follow
// line is named from snapshots copied before the load, so the command never reads a
// map the update loop may be writing.
func (m model) reloadBlocksCmd(changed []filelist.Entry, follow filelist.Entry) tea.Cmd {
	rows := m.allEntries
	opts := m.diffOptions()
	type job struct {
		index  int
		path   string
		staged bool
		before *diff.File
	}
	var work []job
	for _, e := range changed {
		for i, row := range rows {
			if row.Status.Path != e.Status.Path || row.Staged != e.Staged {
				continue
			}
			var before *diff.File
			if entry := m.liveNews[livePath{path: row.Status.Path, staged: row.Staged}]; entry != nil {
				before = entry.diff
			}
			work = append(work, job{index: i, path: row.Status.Path, staged: row.Staged, before: before})
			break
		}
	}
	if len(work) == 0 {
		return nil
	}
	return func() tea.Msg {
		msg := blockUpdateMsg{}
		for _, w := range work {
			f, err := git.FileDiff(w.path, w.staged, opts)
			if err != nil || f == nil {
				continue
			}
			msg.updates = append(msg.updates, diffview.BlockUpdate{Index: w.index, File: f})
			msg.remembers = append(msg.remembers, liveSide{path: w.path, staged: w.staged, file: f})
			if msg.ok || w.path != follow.Status.Path || w.staged != follow.Staged {
				continue
			}
			line, ok := firstChange(f)
			if w.before != nil {
				line, ok = followedChange(w.before, f)
			}
			if ok {
				msg.follow, msg.path, msg.staged, msg.ok = line, w.path, w.staged, true
			}
		}
		return msg
	}
}

// sameRows reports whether two listings name the same rows in the same order, which
// is what lets a live change patch a block instead of rebuilding the document.
func sameRows(a, b []filelist.Entry) bool {
	if len(a) != len(b) || len(a) == 0 {
		return false
	}
	for i := range a {
		if a[i].Status.Path != b[i].Status.Path || a[i].Staged != b[i].Staged {
			return false
		}
	}
	return true
}

// liveSide is one side's diff, carried back from a load to be kept as the next
// baseline.
type liveSide struct {
	path   string
	staged bool
	file   *diff.File
}

// sideRef names a file and one of its diffs.
type sideRef struct {
	path   string
	staged bool
	ok     bool
}

// entryStat is the worktree stat a row carries, or a fresh one for an index row.
func entryStat(e filelist.Entry) (size, mtime int64) {
	if e.Status.ModTime != 0 {
		return e.Status.Size, e.Status.ModTime
	}
	return statKey(e.Status.Path)
}

// forgetDiffs drops the remembered diffs and keeps the stat baselines, so a change of
// diff options does not look like a new edit.
func (m *model) forgetDiffs() {
	for _, entry := range m.liveNews {
		entry.diff = nil
	}
}

// blockSides is the side of each block in a chunk, and the label its band needs.
// Only the two blocks of a partly staged file have to say which half they are.

// duplicatedPaths names the paths the listing holds more than once.

// sideTag is what a band appends to name its half. The glyph goes in the title, not
// in a gutter column: the shapes are ambiguous width.
