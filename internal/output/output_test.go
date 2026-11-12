package output

import (
	"strings"
	"testing"

	"samouly.fun/twig/internal/diff"
)

const patch = "diff --git a/x.go b/x.go\n" +
	"--- a/x.go\n" +
	"+++ b/x.go\n" +
	"@@ -1,2 +1,2 @@\n" +
	" keep\n" +
	"-old\n" +
	"+new\n"

func parsed(t *testing.T) []*diff.File {
	t.Helper()
	d := diff.Parse(patch)
	if len(d.Files) != 1 {
		t.Fatalf("parsed %d files, want 1", len(d.Files))
	}
	return d.Files
}

func TestPatchIsPlainWithoutColor(t *testing.T) {
	got := Patch(parsed(t), false)
	if strings.Contains(got, "\x1b[") {
		t.Fatalf("plain output carries an escape:\n%q", got)
	}
	if !strings.Contains(got, "+new") || !strings.Contains(got, "-old") {
		t.Fatalf("the patch lost its signs:\n%s", got)
	}
}

func TestPatchPaintsByLineKind(t *testing.T) {
	got := Patch(parsed(t), true)

	for _, want := range []string{ansiGreen + "+new", ansiRed + "-old", ansiCyan + "@@"} {
		if !strings.Contains(got, want) {
			t.Fatalf("colored patch is missing %q:\n%q", want, got)
		}
	}
	// The file header stays a header, not an addition or a deletion.
	if !strings.Contains(got, ansiDim+"diff --git") {
		t.Fatalf("the file header is not dimmed:\n%q", got)
	}
	if strings.Contains(got, ansiGreen+"diff --git") || strings.Contains(got, ansiRed+"diff --git") {
		t.Fatalf("the file header was painted as a change:\n%q", got)
	}
}

func TestPatchSkipsAFileWithNoHunks(t *testing.T) {
	file := &diff.File{OldPath: "crlf.txt", NewPath: "crlf.txt", Kind: diff.Modified}
	if got := Patch([]*diff.File{file}, false); got != "" {
		t.Fatalf("a file with no hunk produced a patch: %q", got)
	}
}

// git names both sides in the diff --git line; /dev/null is for --- and +++.
func TestPatchNamesBothSidesOfACreatedOrDeletedFile(t *testing.T) {
	added := &diff.File{Kind: diff.Added, OldPath: "a.go", NewPath: "a.go", NewMode: "100644"}
	added.Hunks = parsed(t)[0].Hunks

	deleted := &diff.File{Kind: diff.Deleted, OldPath: "g.go", NewPath: "g.go", OldMode: "100755"}
	deleted.Hunks = parsed(t)[0].Hunks

	got := Patch([]*diff.File{added, deleted}, false)

	for _, want := range []string{
		"diff --git a/a.go b/a.go\nnew file mode 100644\n--- /dev/null\n+++ b/a.go\n",
		"diff --git a/g.go b/g.go\ndeleted file mode 100755\n--- a/g.go\n+++ /dev/null\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("patch is missing\n%s\ngot:\n%s", want, got)
		}
	}
}

func TestPatchStatesAModeChange(t *testing.T) {
	file := &diff.File{Kind: diff.Modified, OldPath: "s.sh", NewPath: "s.sh", OldMode: "100644", NewMode: "100755"}

	got := Patch([]*diff.File{file}, false)
	want := "diff --git a/s.sh b/s.sh\nold mode 100644\nnew mode 100755\n"
	if got != want {
		t.Fatalf("patch =\n%q\nwant\n%q", got, want)
	}
}

// The hunk structure has to survive the painter for interactive.diffFilter.
func TestPatchKeepsHunkStructure(t *testing.T) {
	const multi = "diff --git a/x.go b/x.go\n" +
		"--- a/x.go\n" +
		"+++ b/x.go\n" +
		"@@ -1,3 +1,3 @@\n" +
		" one\n-two\n+TWO\n three\n" +
		"@@ -20,3 +20,3 @@\n" +
		" twenty\n-old\n+new\n" +
		" tail\n" +
		"diff --git a/y.go b/y.go\n" +
		"--- a/y.go\n" +
		"+++ b/y.go\n" +
		"@@ -5 +5 @@\n" +
		"-a\n+b\n"

	before := diff.Parse(multi)
	after := diff.Parse(Patch(before.Files, false))

	if len(after.Files) != len(before.Files) {
		t.Fatalf("round trip changed the file count: %d, want %d", len(after.Files), len(before.Files))
	}
	for i, want := range before.Files {
		got := after.Files[i]
		if len(got.Hunks) != len(want.Hunks) {
			t.Fatalf("%s: %d hunks after the round trip, want %d", want.Path(), len(got.Hunks), len(want.Hunks))
		}
		for h := range want.Hunks {
			if got.Hunks[h].Header != want.Hunks[h].Header {
				t.Fatalf("%s hunk %d header %q, want %q", want.Path(), h, got.Hunks[h].Header, want.Hunks[h].Header)
			}
			if len(got.Hunks[h].Lines) != len(want.Hunks[h].Lines) {
				t.Fatalf("%s hunk %d has %d lines, want %d", want.Path(), h, len(got.Hunks[h].Lines), len(want.Hunks[h].Lines))
			}
		}
	}
}

func TestPatchPrintsAMergedPathAsItStands(t *testing.T) {
	d := diff.Parse("diff --cc f.go\n--- a/f.go\n+++ b/f.go\n@@@ -1,1 -1,1 +1,2 @@@\n+ a\n++b\n")
	if len(d.Files) != 1 {
		t.Fatalf("parsed %d files, want 1", len(d.Files))
	}

	got := Patch(d.Files, false)
	want := "diff --cc f.go\n@@@ -1,1 -1,1 +1,2 @@@\n+ a\n++b\n"
	if got != want {
		t.Fatalf("patch =\n%q\nwant\n%q", got, want)
	}
}

func TestStripANSILeavesTheTextBehind(t *testing.T) {
	const painted = "\x1b[2mdiff --git a/x.go b/x.go\x1b[0m\n" +
		"\x1b[36m@@ -1 +1 @@\x1b[0m\n" +
		"\x1b[32m+new\x1b[0m\n"

	want := "diff --git a/x.go b/x.go\n@@ -1 +1 @@\n+new\n"
	if got := StripANSI(painted); got != want {
		t.Fatalf("StripANSI = %q, want %q", got, want)
	}
	if got := StripANSI("no colour here"); got != "no colour here" {
		t.Fatalf("StripANSI changed plain text: %q", got)
	}
}
