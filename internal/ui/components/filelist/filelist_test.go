package filelist

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"twig/internal/diff"
	"twig/internal/git"
	"twig/internal/ui/styles"
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
