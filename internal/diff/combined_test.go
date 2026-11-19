package diff

import (
	"strings"
	"testing"
)

func TestCombinedDiffIsReadAsOneDocument(t *testing.T) {
	const cc = "diff --cc f.go\n" +
		"index af70335,f794161..0000000\n" +
		"--- a/f.go\n" +
		"+++ b/f.go\n" +
		"@@@ -1,3 -1,3 +1,7 @@@\n" +
		"  a\n" +
		"++<<<<<<< HEAD\n" +
		" +MAIN\n" +
		"++=======\n" +
		"+ SIDE\n" +
		"++>>>>>>> side\n" +
		"  c\n"

	d := Parse(cc)
	if len(d.Files) != 1 {
		t.Fatalf("parsed %d files, want 1", len(d.Files))
	}
	f := d.Files[0]
	if f.Kind != Unmerged {
		t.Fatalf("kind = %v, want unmerged", f.Kind)
	}
	if f.Path() != "f.go" {
		t.Fatalf("path = %q, want f.go", f.Path())
	}
	if !d.Combined() {
		t.Fatal("Combined() = false on a merged path")
	}
	if len(f.Hunks) != 1 {
		t.Fatalf("%d hunks, want 1", len(f.Hunks))
	}

	lines := f.Hunks[0].Lines
	if len(lines) != 7 {
		t.Fatalf("%d body lines, want 7:\n%v", len(lines), lines)
	}
	want := []struct {
		kind LineKind
		text string
	}{
		{Context, "  a"},
		{Addition, "++<<<<<<< HEAD"},
		{Addition, " +MAIN"},
		{Addition, "++======="},
		{Addition, "+ SIDE"},
		{Addition, "++>>>>>>> side"},
		{Context, "  c"},
	}
	for i, w := range want {
		if lines[i].Kind != w.kind || lines[i].Text != w.text {
			t.Fatalf("line %d = (%v, %q), want (%v, %q)", i, lines[i].Kind, lines[i].Text, w.kind, w.text)
		}
	}
	if lines[0].New != 1 || lines[6].New != 7 {
		t.Fatalf("result numbering = %d..%d, want 1..7", lines[0].New, lines[6].New)
	}
	if lines[0].Old != 0 {
		t.Fatalf("a combined line carries old number %d, want 0", lines[0].Old)
	}
}

func TestOctopusCombinedDiffCountsItsParents(t *testing.T) {
	const octopus = "diff --cc f.go\n" +
		"--- a/f.go\n" +
		"+++ b/f.go\n" +
		"@@@@ -1,1 -1,1 -1,1 +1,2 @@@@\n" +
		"   a\n" +
		"+++b\n"

	d := Parse(octopus)
	if len(d.Files) != 1 || len(d.Files[0].Hunks) != 1 {
		t.Fatalf("parsed %d files, want one with one hunk", len(d.Files))
	}
	lines := d.Files[0].Hunks[0].Lines
	if len(lines) != 2 {
		t.Fatalf("%d body lines, want 2:\n%v", len(lines), lines)
	}
	if lines[1].Kind != Addition || lines[1].Text != "+++b" {
		t.Fatalf("second line = (%v, %q), want an addition of %q", lines[1].Kind, lines[1].Text, "+++b")
	}
}

func TestFormatPatchKeepsTheMailItArrivedIn(t *testing.T) {
	const mail = "From 0123456789abcdef Mon Sep 17 00:00:00 2001\n" +
		"From: Sam <sam@example.com>\n" +
		"Subject: [PATCH] fix the thing\n" +
		"\n" +
		"body line\n" +
		"---\n" +
		" src/a.go | 2 +-\n" +
		"\n" +
		"diff --git a/src/a.go b/src/a.go\n" +
		"--- a/src/a.go\n" +
		"+++ b/src/a.go\n" +
		"@@ -1 +1 @@\n" +
		"-a\n+b\n"

	d := Parse(mail)
	if len(d.Files) != 1 {
		t.Fatalf("parsed %d files, want 1", len(d.Files))
	}
	joined := strings.Join(d.Preamble, "\n")
	for _, want := range []string{"From 0123456789abcdef", "Subject: [PATCH] fix the thing", "body line"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("the mail lost %q:\n%s", want, joined)
		}
	}
	// The blank padding around the mail is not part of it.
	if d.Preamble[0] != "From 0123456789abcdef Mon Sep 17 00:00:00 2001" {
		t.Fatalf("preamble starts with %q", d.Preamble[0])
	}
	if last := d.Preamble[len(d.Preamble)-1]; last != " src/a.go | 2 +-" {
		t.Fatalf("preamble ends with %q", last)
	}
}

func TestPlainDiffCarriesNoPreamble(t *testing.T) {
	d := Parse("diff --git a/x.go b/x.go\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n+b\n")
	if len(d.Preamble) != 0 {
		t.Fatalf("a plain diff carries a preamble: %v", d.Preamble)
	}
	if d.Combined() {
		t.Fatal("Combined() = true on a plain diff")
	}
}
