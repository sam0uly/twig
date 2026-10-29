package git

import (
	"fmt"
	"strings"

	"samouly.fun/twig/internal/diff"
)

// snapshot is one coherent view of the repository state, produced by a
// single `git status --porcelain=v2` run: the staged changes (HEAD vs index)
// and the worktree changes (index vs disk). Everything else reads from it,
// so no operation re-runs a status.
type snapshot struct {
	staged   []FileStatus
	unstaged []FileStatus
	files    map[string]*snapFile
}

// snapFile is what a diff needs to know about one path.
type snapFile struct {
	path      string
	oldPath   string
	staged    bool
	untracked bool

	stagedKind   diff.ChangeKind
	unstagedKind diff.ChangeKind

	// a file whose mode changed but whose content did not still counts as a
	// change, exactly like git's "old mode / new mode" output.
	modeChanged bool

	headOID  string
	indexOID string
}

// snapshotFor returns a snapshot, computing it when missing (or always when
// force is set, which is what a refresh does). Readers that only need the
// current state (a block being re-materialised while painting) take the
// cached pointer without waiting for a status run that is already in flight.
func (rc *repoContext) snapshotFor(force bool) (*snapshot, error) {
	if !force {
		if snap := rc.snapshot.Load(); snap != nil {
			return snap, nil
		}
	}

	rc.mu.Lock()
	defer rc.mu.Unlock()

	if !force {
		if snap := rc.snapshot.Load(); snap != nil {
			return snap, nil
		}
	}

	snap, err := rc.buildSnapshot()
	if err != nil {
		return nil, err
	}
	rc.snapshot.Store(snap)
	return snap, nil
}

// buildSnapshot runs one status pass and parses it. The porcelain v2 format
// carries the index and HEAD blob ids and file modes directly, so the two
// sides of every diff are identified without walking the object database.
func (rc *repoContext) buildSnapshot() (*snapshot, error) {
	out, err := rc.git("status", "--porcelain=v2", "-z",
		"--untracked-files=all", "--find-renames")
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}

	snap := &snapshot{files: make(map[string]*snapFile)}
	stamp := rc.stamp()

	// With -z, each record is NUL-terminated as one token; a rename record
	// is followed by a second token holding the original path.
	tokens := strings.Split(out, "\x00")
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		switch {
		case strings.HasPrefix(tok, "1 "):
			parseOrdinary(snap, strings.SplitN(tok, " ", 9))
		case strings.HasPrefix(tok, "2 "):
			origPath := ""
			if i+1 < len(tokens) {
				origPath = tokens[i+1]
				i++
			}
			parseRename(snap, strings.SplitN(tok, " ", 10), origPath)
		case strings.HasPrefix(tok, "? "):
			path := tok[2:]
			if path == "" {
				continue
			}
			snap.add(path, &snapFile{
				path: path, untracked: true, unstagedKind: diff.Added,
			})
		}
	}

	for _, f := range snap.files {
		if f.staged {
			snap.staged = append(snap.staged, FileStatus{
				Path:    f.path,
				OldPath: f.oldPath,
				Letter:  letterFor(f.stagedKind),
				Staged:  true,
			})
		}
	}
	for _, f := range snap.files {
		if f.untracked {
			snap.unstaged = append(snap.unstaged, FileStatus{
				Path: f.path, Letter: "?", Untracked: true,
			})
			continue
		}
		if f.unstagedKind != diff.Modified && f.unstagedKind != diff.Added && f.unstagedKind != diff.Deleted {
			continue
		}
		snap.unstaged = append(snap.unstaged, FileStatus{
			Path:   f.path,
			Letter: letterFor(f.unstagedKind),
		})
	}

	sortStatus(snap.staged)
	sortStatus(snap.unstaged)

	rc.snapStamp = stamp
	return snap, nil
}

// submoduleOID mode marks a gitlink entry: a submodule, never diffable text.
const submoduleMode = "160000"

// parseOrdinary handles "1" records: one path, index and worktree status
// letters, and the modes and blob ids of all three sides.
func parseOrdinary(snap *snapshot, f []string) {
	if len(f) < 9 {
		return
	}
	xy, mH, mI, mW, hH, hI, path := f[1], f[3], f[4], f[5], f[6], f[7], f[8]
	if path == "" || mH == submoduleMode || mI == submoduleMode {
		return
	}

	// X is the index side: the staged change, if any.
	switch x := xy[0]; x {
	case 'A':
		snap.add(path, &snapFile{
			path: path, staged: true, stagedKind: diff.Added, indexOID: hI,
		})
	case 'D':
		snap.add(path, &snapFile{
			path: path, staged: true, stagedKind: diff.Deleted, headOID: hH,
		})
	case 'R', 'C':
		kind := diff.Renamed
		if x == 'C' {
			kind = diff.Copied
		}
		snap.add(path, &snapFile{
			path: path, staged: true, stagedKind: kind,
			headOID: hH, indexOID: hI,
		})
	case 'M':
		snap.add(path, &snapFile{
			path: path, staged: true, stagedKind: diff.Modified,
			headOID: hH, indexOID: hI, modeChanged: mH != mI,
		})
	}

	// Y is the worktree side: the unstaged change, if any.
	switch y := xy[1]; y {
	case 'M':
		// mW is 000000 when the file is gone from the worktree, which the
		// D branch below reports instead.
		modeChanged := mW != "000000" && mW != mI
		snap.add(path, &snapFile{
			path: path, unstagedKind: diff.Modified,
			indexOID: hI, modeChanged: modeChanged,
		})
	case 'D':
		snap.add(path, &snapFile{
			path: path, unstagedKind: diff.Deleted, indexOID: hI,
		})
	}
}

// parseRename handles "2" records: the path in the record, the original path
// in the following token.
func parseRename(snap *snapshot, f []string, origPath string) {
	if len(f) < 10 || origPath == "" {
		return
	}
	xy, mH, mI, hH, hI, path := f[1], f[3], f[4], f[6], f[7], f[9]
	if path == "" || mH == submoduleMode || mI == submoduleMode {
		return
	}

	if x := xy[0]; x == 'R' || x == 'C' {
		kind := diff.Renamed
		if x == 'C' {
			kind = diff.Copied
		}
		snap.add(path, &snapFile{
			path: path, oldPath: origPath, staged: true, stagedKind: kind,
			headOID: hH, indexOID: hI,
		})
	}

	switch y := xy[1]; y {
	case 'M':
		snap.add(path, &snapFile{path: path, unstagedKind: diff.Modified, indexOID: hI})
	case 'D':
		snap.add(path, &snapFile{path: path, unstagedKind: diff.Deleted, indexOID: hI})
	}
}

// add stores or merges a file entry. A path can be staged and unstaged at
// the same time.
func (s *snapshot) add(path string, f *snapFile) {
	if existing, ok := s.files[path]; ok {
		if f.staged {
			existing.staged = true
			existing.stagedKind = f.stagedKind
			existing.headOID = f.headOID
			existing.indexOID = f.indexOID
			existing.oldPath = f.oldPath
		} else {
			existing.untracked = existing.untracked || f.untracked
			if f.unstagedKind != 0 {
				existing.unstagedKind = f.unstagedKind
			}
		}
		if f.modeChanged {
			existing.modeChanged = true
		}
		return
	}
	s.files[path] = f
}

// fileFor returns the diff inputs for one path.
func (s *snapshot) fileFor(path string, staged bool) (*snapFile, bool) {
	f, ok := s.files[path]
	if !ok {
		return nil, false
	}
	if staged && !f.staged {
		return nil, false
	}
	if !staged && f.stagedOnly() {
		return nil, false
	}
	return f, true
}

func (f *snapFile) stagedOnly() bool {
	return f.staged && !f.untracked && f.unstagedKind == 0
}

// diffFile builds (or returns cached) the diff of one snapshot entry.
func (rc *repoContext) diffFile(f *snapFile, staged bool, opts Options) (*diff.File, error) {
	kind := f.unstagedKind
	if staged {
		kind = f.stagedKind
	}
	if kind == 0 {
		return nil, fmt.Errorf("no change for %q", f.path)
	}

	oldOID, newOID := f.indexOID, ""
	if staged {
		oldOID, newOID = f.headOID, f.indexOID
	}

	key := fmt.Sprintf("%s|%s|%d|%t|%s|%t|%d", oldOID, newOID, opts.Context,
		opts.IgnoreWhitespace, f.path, staged, kind)
	if cached, ok := rc.diffs.get(key); ok {
		return cached, nil
	}

	var oldText, newText string
	var err error
	if staged {
		if oldText, err = rc.blob(oldOID); err != nil {
			return nil, err
		}
		if newText, err = rc.blob(newOID); err != nil {
			return nil, err
		}
	} else {
		if oldText, err = rc.blob(oldOID); err != nil {
			return nil, err
		}
		if kind == diff.Deleted {
			newText = ""
		} else if newText, err = rc.worktreeContent(f.path); err != nil {
			// An unreadable file is an error, not an empty file: showing the
			// whole content as deleted would be a lie.
			return nil, fmt.Errorf("read %q: %w", f.path, err)
		}
	}

	file := diff.Generate(f.path, kind, oldText, newText, generateOptions(opts))
	file.OldPath = f.path
	file.NewPath = f.path
	if staged && f.oldPath != "" {
		file.OldPath = f.oldPath
	}
	if f.modeChanged && len(file.Hunks) == 0 {
		file.Lines = append(file.Lines, diff.Line{
			Kind: diff.NoNewline,
			Text: "file mode changed",
		})
	}
	rc.diffs.put(key, file)
	return file, nil
}

// statusFiles diffs one column of the snapshot.
func (rc *repoContext) statusFiles(staged bool, opts Options) ([]*diff.File, error) {
	snap, err := rc.snapshotFor(false)
	if err != nil {
		return nil, err
	}

	var files []*diff.File
	for _, f := range snap.files {
		if staged != f.staged {
			continue
		}
		if !staged && (f.untracked || f.unstagedKind == 0) {
			continue
		}
		file, err := rc.diffFile(f, staged, opts)
		if err != nil {
			continue
		}
		files = append(files, file)
	}
	sortFiles(files)
	return files, nil
}

func (rc *repoContext) worktreeFiles(opts Options) ([]*diff.File, error) {
	return rc.statusFiles(false, opts)
}

func (rc *repoContext) stagedFiles(opts Options) ([]*diff.File, error) {
	return rc.statusFiles(true, opts)
}

func letterFor(kind diff.ChangeKind) string {
	switch kind {
	case diff.Added:
		return "A"
	case diff.Deleted:
		return "D"
	case diff.Renamed:
		return "R"
	case diff.Copied:
		return "C"
	default:
		return "M"
	}
}

// ---------------------------------------------------------------- commits

// rawChange is one diff-tree entry: two modes, two blob ids, a status letter
// and up to two paths.
type rawChange struct {
	fromMode, toMode string
	fromOID, toOID   string
	status           string
	path, oldPath    string
}

// parseRaw splits NUL-delimited diff-tree output into entries. Each entry is
// one token of five space-separated fields, terminated by a NUL that is
// immediately followed by the path (and, for renames and copies, a second
// token with the original path). The first token of a single-commit run is
// the commit id itself, which carries no colon and is skipped.
func parseRaw(out string) []rawChange {
	var changes []rawChange
	tokens := strings.Split(out, "\x00")
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if !strings.HasPrefix(tok, ":") {
			continue
		}
		f := strings.SplitN(tok, " ", 5)
		if len(f) < 5 || i+1 >= len(tokens) {
			continue
		}
		c := rawChange{
			fromMode: strings.TrimPrefix(f[0], ":"),
			toMode:   f[1],
			fromOID:  f[2],
			toOID:    f[3],
			status:   f[4],
			path:     tokens[i+1],
		}
		i++
		if strings.HasPrefix(c.status, "R") || strings.HasPrefix(c.status, "C") {
			if i+1 < len(tokens) {
				c.oldPath = c.path
				c.path = tokens[i+1]
				i++
			}
		}
		changes = append(changes, c)
	}
	return changes
}

// patchFiles turns raw diff-tree output into diff files. A file whose
// content is missing locally (a shallow or blobless partial clone) becomes a
// one-line band instead of failing the whole view: the listing is still
// true, and the reason is visible where the diff would be.
func (rc *repoContext) patchFiles(out string, opts Options) ([]*diff.File, error) {
	var files []*diff.File
	for _, c := range parseRaw(out) {
		if c.path == "" {
			continue
		}
		if c.fromMode == submoduleMode || c.toMode == submoduleMode {
			files = append(files, diff.Unavailable(c.path, "submodule"))
			continue
		}

		kind := diff.Modified
		oldPath := c.path
		switch {
		case isZeroOID(c.fromOID):
			kind = diff.Added
		case isZeroOID(c.toOID):
			kind = diff.Deleted
		case c.oldPath != "" && c.oldPath != c.path:
			kind = diff.Renamed
			oldPath = c.oldPath
		}

		oldText, err := rc.blob(c.fromOID)
		if err != nil {
			files = append(files, unavailableBlob(c.path, err))
			continue
		}
		newText, err := rc.blob(c.toOID)
		if err != nil {
			files = append(files, unavailableBlob(c.path, err))
			continue
		}

		f := diff.Generate(c.path, kind, oldText, newText, generateOptions(opts))
		f.OldPath = oldPath
		f.NewPath = c.path
		if c.fromMode != c.toMode && len(f.Hunks) == 0 {
			f.Lines = append(f.Lines, diff.Line{
				Kind: diff.NoNewline,
				Text: "file mode changed",
			})
		}
		files = append(files, f)
	}

	sortFiles(files)
	return files, nil
}

// unavailableBlob is the stand-in for a file whose content is not in the
// local object database: a shallow or partial (blobless) clone, or a deleted
// object. One path must not take the whole view down.
func unavailableBlob(path string, err error) *diff.File {
	if isMissingObject(err) {
		return diff.Unavailable(path, "content not present locally")
	}
	return diff.Unavailable(path, err.Error())
}

// isMissingObject reports whether a cat-file failure means the blob is not
// in the local object database, as opposed to a repository-level problem.
func isMissingObject(err error) bool {
	msg := err.Error()
	for _, marker := range []string{
		"bad file",
		"bad object",
		"not a valid object",
		"unable to read",
		"missing blob",
		"does not exist",
		"could not be read",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}
