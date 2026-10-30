package filelist

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	"github.com/charmbracelet/x/ansi"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/ui/styles"
)

func TestViewsNeverExceedPaneHeight(t *testing.T) {
	m := New(0, 0, styles.New())
	m.SetSize(30, 10)

	commits := make([]git.Commit, 50)
	for i := range commits {
		commits[i] = git.Commit{Hash: fmt.Sprint(i), Short: fmt.Sprint(i), Subject: "subject"}
	}
	m.SetCommits(commits)

	files := make([]*diff.File, 50)
	for i := range files {
		files[i] = diff.Generate(fmt.Sprintf("file%d.go", i), diff.Modified, "a\n", "b\n", diff.GenerateOptions{})
	}
	m.SetCommitFiles(files)

	statuses := make([]git.FileStatus, 50)
	for i := range statuses {
		statuses[i] = git.FileStatus{Path: fmt.Sprintf("file%d.go", i), Letter: "M"}
	}
	m.SetFiles(statuses, nil)

	for _, tc := range []struct {
		name string
		view func() string
	}{
		{"files", func() string { m.SetTab(TabFiles); return m.View() }},
		{"commits", func() string { m.SetTab(TabCommits); return m.View() }},
		{"commit files", m.CommitFilesView},
	} {
		if got := len(strings.Split(tc.view(), "\n")); got > 10 {
			t.Fatalf("%s list rendered %d lines into a 10 line pane", tc.name, got)
		}
	}
}

func treeSample() []git.FileStatus {
	return []git.FileStatus{
		{Path: "README.md", Letter: "M"},
		{Path: "internal/ui/tui.go", Letter: "M"},
		{Path: "internal/ui/view.go", Letter: "M"},
		{Path: "internal/diff/myers.go", Letter: "A"},
	}
}

func TestTreeModeBuildsDirectories(t *testing.T) {
	m := New(30, 20, styles.New())
	m.SetSize(30, 20)
	m.SetFiles(nil, treeSample())
	m.SetTree(true)

	items := m.files.Items()
	var dirs, files int
	for _, it := range items {
		switch it.(type) {
		case treeDirItem:
			dirs++
		case fileItem:
			files++
		}
	}
	if dirs != 3 { // internal, internal/ui, internal/diff
		t.Fatalf("got %d directory rows, want 3", dirs)
	}
	if files != 4 {
		t.Fatalf("got %d file rows, want 4", files)
	}

	// File rows carry a base name and a connector prefix in tree mode.
	for _, it := range items {
		f, ok := it.(fileItem)
		if !ok {
			continue
		}
		if f.name == "" || !strings.Contains(f.name, ".") {
			t.Fatalf("file row has no base name: %+v", f)
		}
		if f.prefix == "" {
			t.Fatalf("file row has no connector prefix: %+v", f)
		}
	}
}

func TestTreeCollapseHidesChildren(t *testing.T) {
	m := New(30, 20, styles.New())
	m.SetSize(30, 20)
	m.SetFiles(nil, treeSample())
	m.SetTree(true)

	m.SelectDir("internal/ui")
	before := len(m.files.Items())
	if !m.ToggleSelectedDir() {
		t.Fatal("selected directory was not toggled")
	}
	after := len(m.files.Items())
	if after >= before {
		t.Fatalf("collapsing internal/ui did not hide rows (%d -> %d)", before, after)
	}

	// The cursor stays on the directory it collapsed.
	if d, ok := m.files.SelectedItem().(treeDirItem); !ok || d.path != "internal/ui" {
		t.Fatalf("selection moved away from internal/ui: %+v", m.files.SelectedItem())
	}

	if !m.ToggleSelectedDir() {
		t.Fatal("directory did not expand again")
	}
	if len(m.files.Items()) != before {
		t.Fatalf("expanding did not restore rows (%d -> %d)", before, len(m.files.Items()))
	}
}

func TestFlatModeKeepsFullPaths(t *testing.T) {
	m := New(30, 20, styles.New())
	m.SetSize(30, 20)
	m.SetFiles(nil, treeSample())

	for _, it := range m.files.Items() {
		f, ok := it.(fileItem)
		if !ok {
			continue
		}
		if f.name != "" || f.prefix != "" {
			t.Fatalf("flat row should not use tree fields: %+v", f)
		}
	}
}

func TestTreeToggleKeepsSelection(t *testing.T) {
	m := New(30, 20, styles.New())
	m.SetSize(30, 20)
	m.SetFiles(nil, treeSample())
	m.SelectPath("internal/ui/view.go", false)

	m.SetTree(true)
	if f, _ := m.SelectedFile(); f.Path != "internal/ui/view.go" {
		t.Fatalf("selection lost on tree toggle: %q", f.Path)
	}

	m.SetTree(false)
	if f, _ := m.SelectedFile(); f.Path != "internal/ui/view.go" {
		t.Fatalf("selection lost on flat toggle: %q", f.Path)
	}
}

func TestItemAtRowRespectsPagination(t *testing.T) {
	m := New(30, 20, styles.New())
	m.SetSize(30, 6)
	m.SetFiles(nil, treeSample())

	if _, ok := m.FileAtRow(-1); ok {
		t.Fatal("negative row should not map to an item")
	}
	if _, ok := m.FileAtRow(6); ok {
		t.Fatal("row past the page should not map to an item")
	}
	idx, ok := m.FileAtRow(0)
	if !ok || idx != 0 {
		t.Fatalf("row 0 mapped to %d, %v", idx, ok)
	}
}

func TestSelectedOrdinalSkipsHeadersAndDirs(t *testing.T) {
	m := New(30, 20, styles.New())
	m.SetSize(30, 20)
	m.SetFiles(nil, treeSample())
	m.SetTree(true)

	m.SelectPath("internal/diff/myers.go", false)
	if got := m.SelectedOrdinal(); got != 4 {
		t.Fatalf("ordinal = %d, want 4", got)
	}
}

func TestTreeViewDrawsConnectors(t *testing.T) {
	m := New(40, 20, styles.New())
	m.SetSize(40, 20)
	m.SetFiles(nil, treeSample())
	m.SetTree(true)

	view := ansi.Strip(m.View())
	for _, want := range []string{"internal/", "ui/", "├─ ", "└─ "} {
		if !strings.Contains(view, want) {
			t.Fatalf("tree view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "internal/ui/tui.go") {
		t.Fatal("tree view should show base names, not full paths")
	}
}

// TestCommitRowKeepsTheBodyOutOfTheSidebar pins the commits page split: the
// sidebar shows the subject and metadata, and the commit body is left to the
// diff pane.
func TestCommitRowKeepsTheBodyOutOfTheSidebar(t *testing.T) {
	m := New(60, 12, styles.New())
	m.SetSize(60, 12)
	m.SetCommits([]git.Commit{{
		Hash:        "0123456789012345678901234567890123456789",
		Short:       "0123456",
		Author:      "sam",
		When:        "Oct 29, 26",
		Subject:     "add a body",
		Description: "because the reason matters",
	}})
	m.SetTab(TabCommits)

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "add a body") {
		t.Fatalf("subject missing:\n%s", view)
	}
	if !strings.Contains(view, "0123456 · sam · Oct 29, 26") {
		t.Fatalf("metadata missing:\n%s", view)
	}
	if strings.Contains(view, "because the reason matters") {
		t.Fatalf("commit body leaked into the sidebar:\n%s", view)
	}
}

// TestCommitRowBorderSpansBothLines checks the selected commit's left border is
// drawn on the metadata line as well as the title line.
func TestCommitRowBorderSpansBothLines(t *testing.T) {
	m := New(60, 12, styles.New())
	m.SetSize(60, 12)
	m.SetCommits([]git.Commit{{
		Hash:    "0123456789012345678901234567890123456789",
		Short:   "0123456",
		Author:  "sam",
		When:    "Oct 29, 26",
		Subject: "selected commit",
	}})
	m.SetTab(TabCommits)

	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for i, line := range lines {
		if !strings.Contains(line, "selected commit") {
			continue
		}
		if !strings.Contains(line, verticalLine) {
			t.Fatalf("title line has no border:\n%s", line)
		}
		if i+1 >= len(lines) || !strings.Contains(lines[i+1], verticalLine) {
			t.Fatalf("metadata line has no border:\n%s", strings.Join(lines[i:], "\n"))
		}
		return
	}
	t.Fatalf("selected commit row not found:\n%s", strings.Join(lines, "\n"))
}

// TestFilterRendersWithoutATitleGap pins the filter rendering: the blank title
// row is gone, the input sits directly above the first match, and once the
// filter is applied its text stays on screen so it does not look lost.
func TestFilterRendersWithoutATitleGap(t *testing.T) {
	m := New(30, 8, styles.New())
	m.SetSize(30, 8)
	m.SetFiles(nil, []git.FileStatus{
		{Path: "alpha.go", Letter: "M"},
		{Path: "beta.go", Letter: "M"},
		{Path: "gamma.go", Letter: "M"},
	})

	// While typing: input row, then the first match, nothing between them.
	m.files.SetFilterText("alp")
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(rows[0], "Filter: alp") {
		t.Fatalf("row 0 = %q, want the filter input", rows[0])
	}
	if !strings.Contains(rows[1], "alpha.go") {
		t.Fatalf("row 1 = %q, want the first match under the input", rows[1])
	}
	if got := m.VisibleTotal(); got != 1 {
		t.Fatalf("visible total = %d, want 1", got)
	}
	if got := m.Total(); got != 3 {
		t.Fatalf("total = %d, want the unfiltered 3", got)
	}

	// Applied: the query stays visible and the rows stay put.
	m.files.SetFilterState(list.FilterApplied)
	rows = strings.Split(ansi.Strip(m.View()), "\n")
	if !strings.Contains(rows[0], "Filter: alp") {
		t.Fatalf("applied filter row = %q, want the query kept", rows[0])
	}
	if !strings.Contains(rows[1], "alpha.go") {
		t.Fatalf("applied row 1 = %q, want the match", rows[1])
	}

	// Cleared: back to the full listing.
	m.files.ResetFilter()
	if got := m.VisibleTotal(); got != 3 {
		t.Fatalf("total after clear = %d, want 3", got)
	}
	if rows := strings.Split(ansi.Strip(m.View()), "\n"); strings.Contains(rows[0], "Filter") {
		t.Fatalf("filter row survived the clear: %q", rows[0])
	}
}

// TestStagedAndEditedSection checks a file staged and then edited again gets
// its own section instead of appearing in both lists.
func TestStagedAndEditedSection(t *testing.T) {
	m := New(40, 16, styles.New())
	m.SetSize(40, 16)
	m.SetFiles(
		[]git.FileStatus{{Path: "both.go", Letter: "M", Staged: true}, {Path: "staged.go", Letter: "M", Staged: true}},
		[]git.FileStatus{{Path: "both.go", Letter: "M"}, {Path: "loose.go", Letter: "M"}},
	)

	view := ansi.Strip(m.View())
	for _, want := range []string{"Staged and edited", "Staged changes", "Unstaged changes", "both.go", "staged.go", "loose.go"} {
		if !strings.Contains(view, want) {
			t.Fatalf("listing missing %q:\n%s", want, view)
		}
	}
	// both.go is staged and edited: one row, in the first section, and it must
	// not show up again under unstaged changes.
	if got := strings.Count(view, "both.go"); got != 1 {
		t.Fatalf("both.go appears %d times, want once:\n%s", got, view)
	}
	edited := strings.Index(view, "Staged and edited")
	if next := strings.Index(view, "Staged changes"); edited > next {
		t.Fatalf("sections are out of order:\n%s", view)
	}
}
