package diff

import (
	"fmt"
	"strings"
	"testing"
)

// generalDiff is the same file generated without the tail shortcut, which is what the
// shortcut has to agree with.
func generalDiff(oldText, newText string, opts GenerateOptions, pageLimit int) *File {
	f := &File{OldPath: "a.go", NewPath: "a.go", Kind: Modified}
	buildEdits(f, indexText(oldText), indexText(newText), opts, pageLimit)
	return f
}

// replacementDiff is what a wholesale replacement must produce, built from the script
// written out by hand: every old line gone, then every new line, in that order.
func replacementDiff(oldText, newText string, opts GenerateOptions) *File {
	o, n := indexText(oldText), indexText(newText)
	edits := make([]edit, 0, o.n()+n.n())
	for i := 0; i < o.n(); i++ {
		edits = append(edits, edit{kind: editDelete, oldIdx: int32(i)})
	}
	for i := 0; i < n.n(); i++ {
		edits = append(edits, edit{kind: editInsert, oldIdx: int32(o.n()), newIdx: int32(i)})
	}
	f := &File{OldPath: "a.go", NewPath: "a.go", Kind: Modified}
	install(f, buildPlan(o, n, edits, hunkWindows(edits, o, n, opts.Context)), residentLimit)
	return f
}

func fileShape(f *File) string {
	var b strings.Builder
	fmt.Fprintf(&b, "adds=%d dels=%d\n", f.Additions, f.Deletions)
	for i, h := range f.Hunks {
		fmt.Fprintf(&b, "hunk %d line %d header %q\n", i, h.LineIndex, h.Header)
	}
	for _, l := range f.Lines {
		fmt.Fprintf(&b, "%d %d %d %q\n", l.Kind, l.Old, l.New, l.Text)
	}
	return b.String()
}

func lines(n int, tag string) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%s %d\n", tag, i)
	}
	return b.String()
}

// residentLimit is a page limit large enough that a diff keeps its rendered lines.
const residentLimit = 1 << 20

// TestTailEditMatchesTheGeneralPath: a file that only changed at its end is diffed by
// a shortcut, and the shortcut must produce exactly what matching would have.
func TestTailEditMatchesTheGeneralPath(t *testing.T) {
	const body = "one\ntwo\nthree\nfour\nfive\nsix\nseven\n"
	for _, tc := range []struct {
		name string
		old  string
		new  string
		ctx  int
	}{
		{"append", body, body + "eight\nnine\n", 3},
		{"append without context", body, body + "eight\n", 0},
		{"append one line", body, body + "eight\n", 1},
		{"cut", body, "one\ntwo\nthree\n", 3},
		{"cut without context", body, "one\ntwo\nthree\nfour\nfive\n", 0},
		{"append a lot", lines(40, "l"), lines(200, "l"), 3},
		{"append to a one line file", "only\n", "only\nmore\n", 3},
		{"cut everything but the first line", "a\nb\nc\n", "a\n", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := GenerateOptions{Context: tc.ctx}
			want := generalDiff(tc.old, tc.new, opts, residentLimit)
			if len(want.Lines) == 0 {
				t.Fatal("the matcher produced no rows to compare")
			}
			got := generate("a.go", Modified, tc.old, tc.new, opts, residentLimit)
			if g, w := fileShape(got), fileShape(want); g != w {
				t.Fatalf("shortcut and matcher disagree:\nshortcut:\n%s\nmatcher:\n%s", g, w)
			}
		})
	}
}

// TestTailEditIsNotAShortcutWhenTheLastLineIsOpen: bytes after a last line without
// its terminator extend that line, so they are a rewrite of it, not an append.
func TestTailEditIsNotAShortcutWhenTheLastLineIsOpen(t *testing.T) {
	if _, ok := tailEdit(indexText("one\ntwo"), indexText("one\ntwo three")); ok {
		t.Fatal("an open last line was called a tail append")
	}
	if _, ok := tailEdit(indexText("one\ntwo\n"), indexText("one\ntwo\nthree")); !ok {
		t.Fatal("a terminated last line was not called a tail append")
	}
}

// TestNoCommonLinesMatchesTheGeneralPath: a rewrite from end to end is reported by a
// shortcut, and the shortcut must produce exactly what the hand written script says.
func TestNoCommonLinesMatchesTheGeneralPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		old  string
		new  string
		ctx  int
	}{
		{"small", "a\nb\nc\n", "x\ny\nz\n", 3},
		{"uneven", lines(70, "o"), lines(30, "n"), 3},
		{"no context", "a\nb\nc\n", "x\ny\nz\n", 0},
		{"large", lines(5000, "o"), lines(5000, "n"), 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := GenerateOptions{Context: tc.ctx}
			want := replacementDiff(tc.old, tc.new, opts)
			if len(want.Lines) == 0 {
				t.Fatal("the hand written script produced no rows to compare")
			}
			got := generate("a.go", Modified, tc.old, tc.new, opts, residentLimit)
			if g, w := fileShape(got), fileShape(want); g != w {
				t.Fatalf("shortcut and matcher disagree:\nshortcut:\n%s\nmatcher:\n%s", g, w)
			}
		})
	}
}

// TestNoCommonLinesKeepsASharedLine: one line in common is a line the reader has to
// see as context, so the replacement shortcut must stand down.
func TestNoCommonLinesKeepsASharedLine(t *testing.T) {
	disjoint := generate("a.go", Modified, "a\nb\nc\n", "x\ny\nz\n", GenerateOptions{Context: 3}, residentLimit)
	for _, l := range disjoint.Lines {
		if l.Kind == Context {
			t.Fatalf("a wholesale rewrite showed context line %q", l.Text)
		}
	}

	for _, tc := range []struct {
		name     string
		old, new string
		ignoreWS bool
		context  bool
	}{
		// One line in common has to be shown as context.
		{"a shared line", "a\nb\nc\n", "x\nb\nz\n", false, true},
		// Ignoring whitespace makes the two sides equal, so there is no change at all.
		{"a whitespace only difference", "a\n", "a \n", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := generate("a.go", Modified, tc.old, tc.new, GenerateOptions{Context: 3, IgnoreWhitespace: tc.ignoreWS}, residentLimit)
			seen := false
			for _, l := range f.Lines {
				if l.Kind == Context {
					seen = true
				}
			}
			if seen != tc.context {
				t.Fatalf("%s: context rows = %v, want %v", tc.name, seen, tc.context)
			}
			if !tc.context && f.Additions+f.Deletions != 0 {
				t.Fatalf("%s: reported %d additions and %d deletions, want none",
					tc.name, f.Additions, f.Deletions)
			}
		})
	}
}

// TestNoCommonLinesPagesALongRewrite: the whole file is one replacement, and it is
// paged rather than kept as rows when it is long.
func TestNoCommonLinesPagesALongRewrite(t *testing.T) {
	oldText, newText := lines(600, "o"), lines(600, "n")
	opts := GenerateOptions{Context: 3}
	paged := generate("a.go", Modified, oldText, newText, opts, 16)
	if paged.page == nil {
		t.Fatal("a long rewrite was kept resident instead of paged")
	}
	full := generate("a.go", Modified, oldText, newText, opts, residentLimit)
	if n, m := len(full.Lines), paged.page.rows(); n != m {
		t.Fatalf("paged plan paints %d rows, resident diff has %d", m, n)
	}
	if paged.Additions != 600 || paged.Deletions != 600 {
		t.Fatalf("counts %d/%d, want 600/600", paged.Additions, paged.Deletions)
	}
}

// TestNoCommonLinesPagesALongAppend: a tail edit that renders more rows than a page keeps
// a plan, and the rows the plan paints match the resident representation.
func TestNoCommonLinesPagesALongAppend(t *testing.T) {
	oldText, newText := lines(5, "o"), lines(5, "o")+lines(500, "n")
	opts := GenerateOptions{Context: 3}
	paged := generate("a.go", Modified, oldText, newText, opts, 16)
	if paged.page == nil {
		t.Fatal("a long append was kept resident instead of paged")
	}
	full := generate("a.go", Modified, oldText, newText, opts, residentLimit)
	if n, m := len(full.Lines), paged.page.rows(); n != m {
		t.Fatalf("paged plan paints %d rows, resident diff has %d", m, n)
	}
	if paged.Additions != full.Additions || paged.Deletions != full.Deletions {
		t.Fatalf("paged counts %d/%d, resident %d/%d",
			paged.Additions, paged.Deletions, full.Additions, full.Deletions)
	}
}

// TestRowOfAgreesBetweenPagingAndRendering: a paged diff answers RowOf from its runs,
// while a resident one walks its rows. The two must name the same row, or a follow
// lands beside the change instead of on it.
func TestRowOfAgreesBetweenPagingAndRendering(t *testing.T) {
	var before, after strings.Builder
	for i := 0; i < 4000; i++ {
		if i%7 == 0 {
			fmt.Fprintf(&before, "old %d\n", i)
			fmt.Fprintf(&after, "new %d\n", i)
			continue
		}
		fmt.Fprintf(&before, "same %d\n", i)
		fmt.Fprintf(&after, "same %d\n", i)
	}
	opts := GenerateOptions{Context: 3}
	rendered := generate("a.go", Modified, before.String(), after.String(), opts, residentLimit)
	paged := generate("a.go", Modified, before.String(), after.String(), opts, 32)
	if paged.page == nil {
		t.Fatal("the fixture must be paged")
	}
	if rendered.page != nil {
		t.Fatal("the fixture must be resident to be a reference")
	}

	checked := 0
	for i := range rendered.Hunks {
		h, ok := rendered.HunkAt(i)
		if !ok {
			continue
		}
		for _, l := range h.Lines {
			if l.Kind != Addition && l.Kind != Deletion {
				continue
			}
			want, got := rendered.RowOf(l), paged.RowOf(l)
			if got != want {
				t.Fatalf("line %+v: paged row %d, rendered row %d", l, got, want)
			}
			checked++
		}
	}
	if checked < 100 {
		t.Fatalf("checked %d changed lines, want a document with many", checked)
	}
}
