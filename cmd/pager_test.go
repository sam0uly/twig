package cmd

import (
	"strings"
	"testing"
)

const somePatch = "diff --git a/x.go b/x.go\n" +
	"index 111..222 100644\n" +
	"--- a/x.go\n" +
	"+++ b/x.go\n" +
	"@@ -1,2 +1,2 @@\n" +
	" keep\n" +
	"-old\n" +
	"+new\n"

func TestPagerPassesThroughWhatIsNotAPatch(t *testing.T) {
	const log = "ca8a40d feat(ui): stage a whole folder\n1227a1a fix(license): restore the text\n"

	out, err := runCLI(t, log, "pager")
	if err != nil {
		t.Fatalf("pager: %v", err)
	}
	if out != log {
		t.Fatalf("pager changed what git wrote:\n%q\nwant\n%q", out, log)
	}
}

func TestPagerIsTransparentWhenTheOutputIsPiped(t *testing.T) {
	out, err := runCLI(t, somePatch, "pager")
	if err != nil {
		t.Fatalf("pager: %v", err)
	}
	if out != somePatch {
		t.Fatalf("pager rewrote the patch:\n%q\nwant\n%q", out, somePatch)
	}
}

func TestPagerPaintsAPatchWhenAsked(t *testing.T) {
	out, err := runCLI(t, somePatch, "pager", "--color", "always")
	if err != nil {
		t.Fatalf("pager --color always: %v", err)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("--color always printed no colour:\n%q", out)
	}
	for _, want := range []string{"@@ -1,2 +1,2 @@", "-old", "+new", " keep"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the painted patch lost %q:\n%q", want, out)
		}
	}
}

func TestPagerKeepsHunkCountsAndFileCount(t *testing.T) {
	const two = "diff --git a/x.go b/x.go\n" +
		"--- a/x.go\n" +
		"+++ b/x.go\n" +
		"@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n" +
		"@@ -20,3 +20,3 @@\n twenty\n-old\n+new\n tail\n" +
		"diff --git a/y.go b/y.go\n" +
		"--- a/y.go\n" +
		"+++ b/y.go\n" +
		"@@ -5 +5 @@\n-a\n+b\n"

	out, err := runCLI(t, two, "pager", "--color", "always")
	if err != nil {
		t.Fatalf("pager: %v", err)
	}
	if got := strings.Count(out, "diff --git"); got != 2 {
		t.Fatalf("painted output has %d files, want 2:\n%q", got, out)
	}
	if got := strings.Count(out, "@@ -"); got != 3 {
		t.Fatalf("painted output has %d hunks, want 3:\n%q", got, out)
	}
}

func TestPagerAcceptsAnEmptyStream(t *testing.T) {
	out, err := runCLI(t, "", "pager")
	if err != nil {
		t.Fatalf("pager: %v", err)
	}
	if out != "" {
		t.Fatalf("pager invented output: %q", out)
	}
}

// A combined diff has no plain patch form, so it goes back as git drew it.
func TestPagerDoesNotRepaintACombinedDiff(t *testing.T) {
	const merged = "diff --cc f.go\n" +
		"index 111,222..333\n" +
		"--- a/f.go\n" +
		"+++ b/f.go\n" +
		"@@@ -1,1 -1,1 +1,2 @@@\n" +
		"+ a\n" +
		"++b\n"

	out, err := runCLI(t, merged, "pager", "--color", "always")
	if err != nil {
		t.Fatalf("pager: %v", err)
	}
	if out != merged {
		t.Fatalf("a combined diff was rewritten:\n%q\nwant\n%q", out, merged)
	}
}

func TestPagerReadsAPaintedPatch(t *testing.T) {
	const painted = "\x1b[1mdiff --git a/x.go b/x.go\x1b[m\n" +
		"\x1b[1m--- a/x.go\x1b[m\n" +
		"\x1b[1m+++ b/x.go\x1b[m\n" +
		"\x1b[36m@@ -1,2 +1,2 @@\x1b[m\n" +
		" keep\n" +
		"\x1b[31m-old\x1b[m\n" +
		"\x1b[32m+new\x1b[m\n"

	out, err := runCLI(t, painted, "pager", "--color", "always")
	if err != nil {
		t.Fatalf("pager: %v", err)
	}
	for _, want := range []string{"diff --git a/x.go b/x.go", " keep", "-old", "+new", "@@ -1,2 +1,2 @@"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the repainted patch lost %q:\n%q", want, out)
		}
	}
	if strings.Contains(out, "\x1b[1m") {
		t.Fatalf("the input's own colour survived the repaint:\n%q", out)
	}
}
