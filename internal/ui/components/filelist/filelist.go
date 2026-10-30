// Package filelist renders the Files and Commits listings in soft-serve's
// item style: a colored selector bar on the active row, hash-colored
// metadata and section titles, backed by the bubbles list.
package filelist

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/ui/styles"
)

// Tab indexes.
const (
	TabFiles = iota
	TabCommits
)

// fileItem is a changed file row. In tree mode name holds the base name and
// prefix holds the drawn connectors; in flat mode both are unused.
type fileItem struct {
	status git.FileStatus
	staged bool
	name   string
	prefix string
}

func (i fileItem) FilterValue() string { return i.status.Path }

// treeDirItem is a directory row in tree mode. Directories are selectable so
// they can be expanded and collapsed, but they never load a diff.
type treeDirItem struct {
	path      string
	name      string
	prefix    string
	collapsed bool
}

func (d treeDirItem) FilterValue() string { return d.path }

// commitItem is a commit row.
type commitItem struct{ commit git.Commit }

func (c commitItem) Title() string       { return c.commit.Subject }
func (c commitItem) Description() string { return describe(c.commit) }
func (c commitItem) FilterValue() string {
	// The commit filter covers what identifies a commit: its message, its
	// author, the full and short hashes, and the date as shown.
	return strings.Join([]string{
		c.commit.Subject, c.commit.Description, c.commit.Author,
		c.commit.Short, c.commit.Hash, c.commit.When,
	}, " ")
}

func describe(c git.Commit) string {
	return fmt.Sprintf("%s · %s · %s", c.Short, c.Author, c.When)
}

// commitFileItem is a file row in the per-commit sidebar.
type commitFileItem struct{ file *diff.File }

func (c commitFileItem) FilterValue() string { return c.file.Path() }

// sectionItem is a non-selectable section header row.
type sectionItem struct{ title string }

func (s sectionItem) FilterValue() string { return "" }

// Model is the Files/Commits listing, including the per-commit file
// sidebar.
type Model struct {
	files       list.Model
	commits     list.Model
	commitFiles list.Model
	styles      styles.Styles
	width       int
	height      int
	tab         int

	// tree renders the file listing as a directory tree; collapsed maps
	// directory paths to their collapsed state.
	tree      bool
	collapsed map[string]bool
	staged    []git.FileStatus
	unstaged  []git.FileStatus
}

// New builds the listing.
func New(width, height int, st styles.Styles) Model {
	return Model{
		files:       newGlowList(glowDelegate{styles: st, isCommit: false}, st),
		commits:     newGlowList(glowDelegate{styles: st, isCommit: true}, st),
		commitFiles: newGlowList(glowDelegate{styles: st, isCommitFile: true}, st),
		styles:      st,
		width:       width,
		height:      height,
		collapsed:   make(map[string]bool),
	}
}

func newGlowList(d glowDelegate, st styles.Styles) list.Model {
	l := list.New(nil, d, 0, 0)
	// The bubbles list quits on "v" by default, which would exit twig
	// whenever the sidebar has focus; twig owns quitting.
	l.DisableQuitKeybindings()
	l.SetShowStatusBar(false)
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	// Pagination is off on purpose: the bubbles list reserves a row for the
	// paginator even when it renders nothing, which made the sidebar grow one
	// line past its pane and clip the bottom border. The footer already shows
	// the cursor position.
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)
	l.Filter = exactFilter
	l.InfiniteScrolling = true
	l.Styles.PaginationStyle = st.Pagination()
	l.Styles.NoItems = st.Subtle()
	l.Styles.DefaultFilterCharacterMatch = st.SelectedTab()
	return l
}

// SetStyles updates the theme.
func (m *Model) SetStyles(st styles.Styles) {
	m.styles = st
	for _, l := range []*list.Model{&m.files, &m.commits, &m.commitFiles} {
		l.Styles.PaginationStyle = st.Pagination()
		l.Styles.NoItems = st.Subtle()
		l.Styles.DefaultFilterCharacterMatch = st.SelectedTab()
	}
}

// SetSize resizes the lists.
func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	// The list always renders an (empty) title section first; size it one row
	// taller and drop that row when rendering, so no blank line is wasted.
	m.files.SetSize(width, height+1)
	m.commits.SetSize(width, height+1)
	m.commitFiles.SetSize(width, height+1)
}

// SetCommitFilesSize resizes only the per-commit file sidebar.
func (m *Model) SetCommitFilesSize(width, height int) {
	m.commitFiles.SetSize(width, height+1)
}

// SetTab switches the visible tab.
func (m *Model) SetTab(tab int) { m.tab = tab }

// Tab returns the active tab.
func (m Model) Tab() int { return m.tab }

// NextTab cycles to the next tab.
func (m *Model) NextTab() { m.tab = (m.tab + 1) % 2 }

// SetFiles replaces the file listing with staged and unstaged sections,
// keeping the selection on the same path and staging state when possible.
func (m *Model) SetFiles(staged, unstaged []git.FileStatus) {
	m.staged, m.unstaged = staged, unstaged
	m.rebuildFiles()
}

// SetTree switches between the flat path listing and the directory tree.
func (m *Model) SetTree(tree bool) {
	if m.tree == tree {
		return
	}
	m.tree = tree
	m.rebuildFiles()
}

// ToggleTree flips between flat and tree rendering.
func (m *Model) ToggleTree() { m.SetTree(!m.tree) }

// Tree reports whether the tree rendering is active.
func (m Model) Tree() bool { return m.tree }

// rebuildFiles regenerates the item list for the current mode, keeping the
// cursor on the same file or directory.
func (m *Model) rebuildFiles() {
	prev, _ := m.SelectedFile()
	prevDir := ""
	if d, ok := m.files.SelectedItem().(treeDirItem); ok {
		prevDir = d.path
	}

	m.files.SetItems(m.fileItems())

	switch {
	case prevDir != "":
		m.SelectDir(prevDir)
	case prev.Path != "":
		m.SelectPath(prev.Path, prev.Staged)
	}
	// Never leave the cursor parked on a section header.
	if it := m.files.SelectedItem(); it != nil {
		if _, ok := it.(sectionItem); ok {
			m.selectNearestFile()
		}
	}
}

// fileItems builds the item list for the current rendering mode.
func (m *Model) fileItems() []list.Item {
	var items []list.Item
	add := func(title string, files []git.FileStatus, staged bool) {
		if len(files) == 0 {
			return
		}
		items = append(items, sectionItem{title: title})
		if m.tree {
			items = append(items, buildTreeItems(files, staged, m.collapsed)...)
			return
		}
		for _, f := range files {
			items = append(items, fileItem{status: f, staged: staged})
		}
	}
	// A file can be staged and edited again afterwards; that is "MM" in git's
	// own status. It gets a section of its own instead of appearing twice.
	stagedPaths := make(map[string]bool, len(m.staged))
	for _, f := range m.staged {
		stagedPaths[f.Path] = true
	}
	var both, stagedOnly []git.FileStatus
	for _, f := range m.staged {
		if f.Untracked {
			continue
		}
		edited := false
		for _, u := range m.unstaged {
			if !u.Untracked && u.Path == f.Path {
				edited = true
				break
			}
		}
		if edited {
			both = append(both, f)
		} else {
			stagedOnly = append(stagedOnly, f)
		}
	}
	var unstagedOnly []git.FileStatus
	for _, f := range m.unstaged {
		if !f.Untracked && stagedPaths[f.Path] {
			continue // shown in "Staged and edited"
		}
		unstagedOnly = append(unstagedOnly, f)
	}

	add("Staged and edited", both, true)
	add("Staged changes", stagedOnly, true)
	add("Unstaged changes", unstagedOnly, false)
	return items
}

// buildTreeItems flattens the file paths into a directory tree, honouring
// collapsed directories and drawing box connectors for each row.
func buildTreeItems(files []git.FileStatus, staged bool, collapsed map[string]bool) []list.Item {
	type node struct {
		name     string
		path     string
		dir      bool
		file     git.FileStatus
		children []*node
	}

	root := &node{}
	dirs := map[string]*node{"": root}

	for _, f := range files {
		parts := strings.Split(f.Path, "/")
		parent := root
		cur := ""
		for _, part := range parts[:len(parts)-1] {
			if cur == "" {
				cur = part
			} else {
				cur += "/" + part
			}
			n, ok := dirs[cur]
			if !ok {
				n = &node{name: part, path: cur, dir: true}
				dirs[cur] = n
				parent.children = append(parent.children, n)
			}
			parent = n
		}
		parent.children = append(parent.children, &node{
			name: parts[len(parts)-1],
			path: f.Path,
			file: f,
		})
	}

	var items []list.Item
	var walk func(nodes []*node, prefix string)
	walk = func(nodes []*node, prefix string) {
		for i, n := range nodes {
			last := i == len(nodes)-1
			conn, childPrefix := "├─ ", prefix+"│  "
			if last {
				conn, childPrefix = "└─ ", prefix+"   "
			}
			if n.dir {
				items = append(items, treeDirItem{
					path:      n.path,
					name:      n.name,
					prefix:    prefix + conn,
					collapsed: collapsed[n.path],
				})
				if !collapsed[n.path] {
					walk(n.children, childPrefix)
				}
				continue
			}
			items = append(items, fileItem{
				status: n.file,
				staged: staged,
				name:   n.name,
				prefix: prefix + conn,
			})
		}
	}
	walk(root.children, "")

	return items
}

// SelectDir moves the cursor to the directory with the given path.
func (m *Model) SelectDir(path string) {
	for i, it := range visible(m.files) {
		if d, ok := it.(treeDirItem); ok && d.path == path {
			m.files.Select(i)
			return
		}
	}
}

// ToggleSelectedDir expands or collapses the selected directory. It reports
// whether the selection was a directory.
func (m *Model) ToggleSelectedDir() bool {
	d, ok := m.files.SelectedItem().(treeDirItem)
	if !ok {
		return false
	}
	m.collapsed[d.path] = !m.collapsed[d.path]
	m.rebuildFiles()
	return true
}

// selectNearestFile moves the cursor to the closest selectable file row.
func (m *Model) selectNearestFile() {
	items := visible(m.files)
	cur := m.files.Index()
	for d := 1; d < len(items); d++ {
		if cur+d < len(items) {
			if _, ok := items[cur+d].(fileItem); ok {
				m.files.Select(cur + d)
				return
			}
		}
		if cur-d >= 0 {
			if _, ok := items[cur-d].(fileItem); ok {
				m.files.Select(cur - d)
				return
			}
		}
	}
}

// SetCommitFiles replaces the per-commit file sidebar.
func (m *Model) SetCommitFiles(files []*diff.File) {
	items := make([]list.Item, len(files))
	for i, f := range files {
		items[i] = commitFileItem{file: f}
	}
	m.commitFiles.SetItems(items)
}

// SelectedCommitFile returns the selected file in the per-commit sidebar.
func (m *Model) SelectedCommitFile() (*diff.File, bool) {
	it := m.commitFiles.SelectedItem()
	if it == nil {
		return nil, false
	}
	if f, ok := it.(commitFileItem); ok {
		return f.file, true
	}
	return nil, false
}

// SelectCommitFile moves the cursor to the given index.
func (m *Model) SelectCommitFile(idx int) {
	if idx >= 0 && idx < len(visible(m.commitFiles)) {
		m.commitFiles.Select(idx)
	}
}

// CommitFilesTotal returns the number of files in the commit sidebar.
func (m *Model) CommitFilesTotal() int {
	return len(visible(m.commitFiles))
}

// CommitFilesIndex returns the cursor index of the commit file sidebar.
func (m *Model) CommitFilesIndex() int {
	return m.commitFiles.Index()
}

// Select moves the cursor to the given item index in the files list.
func (m *Model) Select(idx int) {
	if idx >= 0 && idx < len(visible(m.files)) {
		m.files.Select(idx)
	}
}

// FileAtRow maps a row inside the files sidebar body to an item index.
func (m Model) FileAtRow(row int) (int, bool) {
	return itemAtRow(m.files, row)
}

// CommitFileAt maps a row inside the per-commit sidebar body to an item
// index.
func (m Model) CommitFileAt(row int) (int, bool) {
	return itemAtRow(m.commitFiles, row)
}

// CommitAtRow maps a row inside the commits sidebar body to a commit index. A
// commit row is a subject line and a metadata line, so a click on either one
// selects that commit.
func (m Model) CommitAtRow(row int) (int, bool) {
	const perItem = 2
	per := m.commits.Paginator.PerPage
	if per <= 0 || row < 0 || row >= per*perItem {
		return 0, false
	}
	idx := m.commits.Paginator.Page*per + row/perItem
	if idx < 0 || idx >= len(visible(m.commits)) {
		return 0, false
	}
	return idx, true
}

// SelectCommit moves the commit cursor to the given index.
func (m *Model) SelectCommit(idx int) {
	if idx >= 0 && idx < len(visible(m.commits)) {
		m.commits.Select(idx)
	}
}

// itemAtRow converts a click row into an item index, accounting for the list
// pagination that scrolls it.
func itemAtRow(l list.Model, row int) (int, bool) {
	per := l.Paginator.PerPage
	if per <= 0 || row < 0 || row >= per {
		return 0, false
	}
	idx := l.Paginator.Page*per + row
	if idx < 0 || idx >= len(visible(l)) {
		return 0, false
	}
	return idx, true
}

// SelectedOrdinal returns the 1-based position of the cursor among the file
// rows only, ignoring section headers and directories.
func (m *Model) SelectedOrdinal() int {
	cur := m.files.Index()
	n := 0
	for i, it := range visible(m.files) {
		if _, ok := it.(fileItem); !ok {
			continue
		}
		n++
		if i == cur {
			return n
		}
	}
	return max(1, n)
}

// SetCommits replaces the commit list.
func (m *Model) SetCommits(commits []git.Commit) {
	items := make([]list.Item, len(commits))
	for i, c := range commits {
		items[i] = commitItem{commit: c}
	}
	m.commits.SetItems(items)
}

// SelectedFile returns the currently selected file, or the zero value.
func (m *Model) SelectedFile() (git.FileStatus, bool) {
	it := m.files.SelectedItem()
	if it == nil {
		return git.FileStatus{}, false
	}
	if f, ok := it.(fileItem); ok {
		return f.status, f.staged
	}
	return git.FileStatus{}, false
}

// SelectPath moves the cursor to the file with the given path.
func (m *Model) SelectPath(path string, staged bool) {
	for i, it := range visible(m.files) {
		if f, ok := it.(fileItem); ok && f.status.Path == path && f.staged == staged {
			m.files.Select(i)
			return
		}
	}
	// Fall back to any entry of the same path.
	for i, it := range visible(m.files) {
		if f, ok := it.(fileItem); ok && f.status.Path == path {
			m.files.Select(i)
			return
		}
	}
}

// SelectedCommit returns the selected commit on the commits tab.
func (m *Model) SelectedCommit() (git.Commit, bool) {
	it := m.commits.SelectedItem()
	if it == nil {
		return git.Commit{}, false
	}
	if c, ok := it.(commitItem); ok {
		return c.commit, true
	}
	return git.Commit{}, false
}

// VisibleTotal is the number of selectable items the visible tab is showing,
// which is what the footer and the position counter report while a filter is
// applied.
func (m *Model) VisibleTotal() int {
	if m.tab == TabFiles {
		return countSelectable(visible(m.files))
	}
	return len(visible(m.commits))
}

// exactFilter matches targets as case-insensitive substrings, or as regular
// expressions when the term contains metacharacters. The fuzzy default scatters
// matches across unrelated commits, which reads as "inaccurate" for hashes,
// dates and names.
func exactFilter(term string, targets []string) []list.Rank {
	if term == "" {
		return nil
	}
	var rx *regexp.Regexp
	if strings.ContainsAny(term, `\.+*?()[]{}^$|`) {
		rx, _ = regexp.Compile("(?i)" + term)
	}
	lower := strings.ToLower(term)

	out := make([]list.Rank, 0, len(targets))
	for i, t := range targets {
		switch {
		case rx != nil:
			if rx.MatchString(t) {
				out = append(out, list.Rank{Index: i})
			}
		default:
			if strings.Contains(strings.ToLower(t), lower) {
				out = append(out, list.Rank{Index: i})
			}
		}
	}
	return out
}

// CommitFilesFilterText is the per-commit sidebar's filter query.
func (m *Model) CommitFilesFilterText() string { return m.commitFiles.FilterInput.Value() }

// CommitFilesFilterState reports the per-commit sidebar's filter state.
func (m *Model) CommitFilesFilterState() list.FilterState {
	return m.commitFiles.FilterState()
}

// SidebarFiltered reports whether the visible list has a filter on, applied or
// still being typed.
func (m *Model) SidebarFiltered() bool { return m.FilterStateDebug() != list.Unfiltered }

// CommitFilesFiltering reports whether the per-commit sidebar is capturing
// keystrokes into its filter input.
func (m *Model) CommitFilesFiltering() bool {
	return m.commitFiles.FilterState() == list.Filtering
}

// CommitFilesFiltered reports whether the per-commit sidebar has a filter on,
// applied or still being typed.
func (m *Model) CommitFilesFiltered() bool {
	return m.commitFiles.FilterState() != list.Unfiltered
}

// FilterStateDebug reports the visible list's filter state, for tests.
func (m *Model) FilterStateDebug() list.FilterState {
	if m.tab == TabFiles {
		return m.files.FilterState()
	}
	return m.commits.FilterState()
}

// visible returns the items the list actually shows: the filtered set while a
// filter is on, everything otherwise. Row, ordinal and selection maths all go
// through it so they stay in step with the screen.
func visible(l list.Model) []list.Item { return l.VisibleItems() }

// FilterActive reports whether the visible list is filtering.
func (m *Model) FilterActive() bool {
	if m.tab == TabFiles {
		return m.files.FilterState() == list.Filtering
	}
	return m.commits.FilterState() == list.Filtering
}

// Total returns the number of selectable items on the visible tab.
func (m *Model) Total() int {
	if m.tab == TabFiles {
		return countSelectable(m.files.Items())
	}
	return len(m.commits.Items())
}

// tailTruncate shortens a path from the left, keeping the file name visible:
// "…/components/diffview.go" reads better than "internal/ui/compo…".
func tailTruncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	const ellipsis = "…"
	return ansi.TruncateLeft(s, ansi.StringWidth(s)-w+ansi.StringWidth(ellipsis), ellipsis)
}

func countSelectable(items []list.Item) int {
	n := 0
	for _, it := range items {
		if _, ok := it.(fileItem); ok {
			n++
		}
	}
	return n
}

// SelectedIndex returns the index of the selected item, or -1.
func (m *Model) SelectedIndex() int {
	if m.tab == TabFiles {
		return m.files.Index()
	}
	return m.commits.Index()
}

// Entry is a selectable file row with its staging state.
type Entry struct {
	Status git.FileStatus
	Staged bool
}

// NeighborEntries returns up to n file rows above and below the cursor,
// nearest first, skipping section headers.
func (m *Model) NeighborEntries(n int) []Entry {
	items := visible(m.files)
	cur := m.files.Index()

	var out []Entry
	for d := 1; d <= n; d++ {
		if i := cur + d; i >= 0 && i < len(items) {
			if f, ok := items[i].(fileItem); ok {
				out = append(out, Entry{Status: f.status, Staged: f.staged})
			}
		}
		if i := cur - d; i >= 0 && i < len(items) {
			if f, ok := items[i].(fileItem); ok {
				out = append(out, Entry{Status: f.status, Staged: f.staged})
			}
		}
	}
	return out
}

// selectable reports whether a row can hold the cursor.
func (m *Model) selectable(it list.Item) bool {
	switch it.(type) {
	case fileItem:
		return true
	case treeDirItem:
		return m.tree
	}
	return false
}

// MoveSelection moves the cursor by delta rows, clamped.
func (m *Model) MoveSelection(delta int) {
	l := &m.files
	if m.tab == TabCommits {
		l = &m.commits
	}
	total := len(visible(*l))
	if total == 0 {
		return
	}
	idx := min(max(l.Index()+delta, 0), total-1)
	l.Select(idx)
}

// StagedCount returns the number of staged files.
func (m *Model) StagedCount() int {
	n := 0
	for _, it := range visible(m.files) {
		if f, ok := it.(fileItem); ok && f.staged {
			n++
		}
	}
	return n
}

// StagedFiles returns the staged file statuses, in listing order.
func (m Model) StagedFiles() []git.FileStatus {
	out := make([]git.FileStatus, len(m.staged))
	copy(out, m.staged)
	return out
}

// OrderedEntries returns every file row in display order, across sections and
// tree levels, with its staging state. The all-changes view uses it so each
// row maps onto the aggregate's file band.
func (m Model) OrderedEntries() []Entry {
	var out []Entry
	for _, it := range visible(m.files) {
		if f, ok := it.(fileItem); ok {
			out = append(out, Entry{Status: f.status, Staged: f.staged})
		}
	}
	return out
}

// Update processes messages for the visible list.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.tab == TabFiles {
		// Section headers are labels, not rows: arrow keys must step over
		// them so the diff pane always follows a real file.
		if km, ok := msg.(tea.KeyPressMsg); ok && m.files.FilterState() == list.Unfiltered {
			switch km.String() {
			case "up", "k":
				m.moveSelectable(-1)
				return m, nil
			case "down", "j":
				m.moveSelectable(1)
				return m, nil
			}
		}
		m.files, cmd = m.files.Update(msg)
	} else {
		m.commits, cmd = m.commits.Update(msg)
	}
	return m, cmd
}

// MoveSelectable moves the cursor by delta selectable rows and reports
// whether it moved. Section headers are always skipped; directory rows are
// selectable only in tree mode.
func (m *Model) MoveSelectable(delta int) bool { return m.moveSelectable(delta) }

// moveSelectable moves the cursor by delta rows, skipping section headers
// (and, outside tree mode, directory rows) and stopping at the ends.
func (m *Model) moveSelectable(delta int) bool {
	items := visible(m.files)
	for i := m.files.Index() + delta; i >= 0 && i < len(items); i += delta {
		if m.selectable(items[i]) {
			m.files.Select(i)
			return true
		}
	}
	return false
}

// View renders the list for the active tab.
func (m Model) View() string {
	if m.tab == TabFiles {
		return renderList(m.files, m.height, m.styles)
	}
	return renderList(m.commits, m.height, m.styles)
}

// CommitFilesView renders the per-commit file sidebar.
func (m Model) CommitFilesView() string {
	return renderList(m.commitFiles, m.height, m.styles)
}

// renderList renders a list into exactly height rows. The bubbles list always
// reserves its first row for the title/filter section; when nothing is being
// filtered that row is empty, so it is dropped here. While filtering, the row
// holds the filter input and the last row is trimmed instead.
func renderList(l list.Model, height int, st styles.Styles) string {
	view := l.View()
	lines := strings.Split(view, "\n")

	// The list reserves a row for its title above the items. It is blank
	// because titles are off, so drop it: while filtering that row would sit
	// between the filter input and the first match, and unfiltered it would
	// push the whole listing down one row.
	if len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[0])) == "" {
		lines = lines[1:]
	} else if len(lines) > 1 && strings.TrimSpace(ansi.Strip(lines[1])) == "" &&
		l.FilterState() != list.Unfiltered {
		// Filtering: row 0 is the input, the blank title row follows it.
		lines = append(lines[:1], lines[2:]...)
	}

	// With the filter applied the list renders no summary row (titles and the
	// status bar are off), so the filter would be invisible and look lost.
	// Keep it on screen until it is cleared.
	if l.FilterState() == list.FilterApplied {
		if q := strings.TrimSpace(l.FilterInput.Value()); q != "" {
			// Same shape as the diff pane's applied search line: the query and
			// how much it left, muted.
			line := " Filter: " + q + "  " + countLabel(countSelectable(l.VisibleItems()))
			lines = append([]string{ansi.Truncate(st.Subtle().Render(line), max(1, l.Width()), "…")}, lines...)
		}
	}

	// Two-line commit rows cannot fill an odd pane, and the bubbles list cuts
	// the second line of the row it lands on. Trim to the row boundary and
	// paint the leftover row in the list background instead of leaving an
	// unstyled gap at the bottom.
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	blank := strings.Repeat(" ", max(1, l.Width()))
	for height > 0 && len(lines) < height {
		lines = append(lines, blank)
	}
	return strings.Join(lines, "\n")
}

// countLabel renders "1 file" or "3 files".
func countLabel(n int) string {
	if n == 1 {
		return "1 file"
	}
	return strconv.Itoa(n) + " files"
}

// UpdateCommitFiles processes messages for the per-commit file sidebar.
func (m Model) UpdateCommitFiles(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.commitFiles, cmd = m.commitFiles.Update(msg)
	return m, cmd
}

// FilterKeys returns the filter binding for help display.
func (m *Model) FilterKeys() []key.Binding {
	return []key.Binding{m.files.KeyMap.Filter}
}

// glowDelegate renders rows in soft-serve's LogItem style: a colored
// selector bar, hash-colored metadata and title colors.
type glowDelegate struct {
	styles       styles.Styles
	isCommit     bool
	isCommitFile bool
}

func (d glowDelegate) Height() int {
	if d.isCommit {
		return 2
	}
	return 1
}

func (d glowDelegate) Spacing() int { return 0 }

func (d glowDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

const verticalLine = "┃"

func (d glowDelegate) Render(w io.Writer, m list.Model, index int, value list.Item) {
	st := d.styles

	switch v := value.(type) {
	case sectionItem:
		_, _ = fmt.Fprintf(w, " %s", st.Subtle().Bold(true).Render(v.title))
		return

	case fileItem:
		rowWidth := max(1, m.Width()-2)
		name := v.status.DisplayPath()
		if v.name != "" {
			name = v.name
		}
		conn := ""
		if v.prefix != "" {
			conn = st.Subtle().Render(v.prefix)
		}
		avail := max(1, rowWidth-4-lipgloss.Width(v.prefix))
		if v.name == "" {
			name = tailTruncate(name, avail)
		} else {
			name = ansi.Truncate(name, avail, "…")
		}
		letter := v.status.Letter
		if v.status.Untracked {
			letter = "?"
		}
		letterStyle := lipgloss.NewStyle().Foreground(st.HashFg())
		titleStyle := lipgloss.NewStyle().Foreground(st.TitleNormalFg())
		if index == m.Index() {
			letterStyle = lipgloss.NewStyle().Foreground(st.HashActiveFg()).Bold(true)
			titleStyle = lipgloss.NewStyle().Foreground(st.TitleActiveFg()).Bold(true)
			_, _ = fmt.Fprintf(
				w, "%s %s%s%s",
				lipgloss.NewStyle().Foreground(st.SelectorFg()).Render(verticalLine),
				conn,
				letterStyle.Render(letter+" "),
				titleStyle.Render(name),
			)
		} else {
			_, _ = fmt.Fprintf(
				w, "  %s%s%s",
				conn,
				letterStyle.Render(letter+" "),
				titleStyle.Render(name),
			)
		}
		return

	case treeDirItem:
		rowWidth := max(1, m.Width()-2)
		glyph := "▾ "
		if v.collapsed {
			glyph = "▸ "
		}
		conn := st.Subtle().Render(v.prefix)
		avail := max(1, rowWidth-4-lipgloss.Width(v.prefix))
		name := ansi.Truncate(v.name+"/", avail, "…")

		glyphStyle := lipgloss.NewStyle().Foreground(st.HashFg())
		nameStyle := lipgloss.NewStyle().Foreground(st.TitleNormalFg())
		if index == m.Index() {
			glyphStyle = lipgloss.NewStyle().Foreground(st.HashActiveFg()).Bold(true)
			nameStyle = lipgloss.NewStyle().Foreground(st.TitleActiveFg()).Bold(true)
			_, _ = fmt.Fprintf(
				w, "%s %s%s%s",
				lipgloss.NewStyle().Foreground(st.SelectorFg()).Render(verticalLine),
				conn,
				glyphStyle.Render(glyph),
				nameStyle.Render(name),
			)
		} else {
			_, _ = fmt.Fprintf(
				w, "  %s%s%s",
				conn,
				glyphStyle.Render(glyph),
				nameStyle.Render(name),
			)
		}
		return

	case commitFileItem:
		rowWidth := max(1, m.Width()-2)
		path := tailTruncate(v.file.Path(), max(1, rowWidth-12))
		// A lazily loaded commit knows the paths first and the counts only
		// once a file's diff has been built, so an unknown count stays blank
		// instead of claiming zero.
		stats := ""
		if v.file.Additions >= 0 && v.file.Deletions >= 0 {
			stats = fmt.Sprintf("+%d -%d", v.file.Additions, v.file.Deletions)
		}
		pad := max(0, rowWidth-lipgloss.Width(path)-lipgloss.Width(stats)-3)
		if index == m.Index() {
			_, _ = fmt.Fprintf(
				w, "%s %s%s%s",
				lipgloss.NewStyle().Foreground(st.SelectorFg()).Render(verticalLine),
				lipgloss.NewStyle().Foreground(st.TitleActiveFg()).Bold(true).Render(" "+path),
				strings.Repeat(" ", pad),
				lipgloss.NewStyle().Foreground(st.DescActiveFg()).Render(" "+stats),
			)
		} else {
			_, _ = fmt.Fprintf(
				w, "  %s%s%s",
				lipgloss.NewStyle().Foreground(st.TitleNormalFg()).Render(path),
				strings.Repeat(" ", pad+1),
				lipgloss.NewStyle().Foreground(st.DescNormalFg()).Render(stats),
			)
		}
		return

	case commitItem:
		rowWidth := max(1, m.Width()-2)
		title := ansi.Truncate(v.commit.Subject, max(1, rowWidth-2), "…")
		meta := ansi.Truncate(describe(v.commit), max(1, rowWidth-2), "…")

		titleStyle := lipgloss.NewStyle().Foreground(st.TitleNormalFg())
		metaStyle := lipgloss.NewStyle().Foreground(st.DescNormalFg())
		// The border runs the full height of the row, so both the title and
		// the metadata line carry it.
		selector := "  "
		if index == m.Index() {
			titleStyle = lipgloss.NewStyle().Foreground(st.TitleActiveFg()).Bold(true)
			metaStyle = lipgloss.NewStyle().Foreground(st.DescActiveFg())
			selector = lipgloss.NewStyle().Foreground(st.SelectorFg()).Render(verticalLine) + " "
		}

		_, _ = fmt.Fprintf(w, "%s%s\n%s%s",
			selector, titleStyle.Render(title),
			selector, metaStyle.Render(meta))
	}
}
