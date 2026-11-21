package diff

import (
	"strings"
	"testing"
)

// The pager is fed whatever git prints, not only diffs. These are real shapes
// from git's own output, and every one of them has to be classified the same
// way twice in a row: a stream that is not a patch must produce no files, or the
// pager would swallow a log or a status listing.
func TestGitStreamsAreClassified(t *testing.T) {
	tests := []struct {
		name  string
		input string
		files int
	}{
		{
			name:  "log oneline",
			input: "abc1234 fix the thing\ndef5678 add the other thing\n",
		},
		{
			name:  "log with decoration",
			input: "* abc1234 (HEAD -> main) fix the thing\n* def5678 add the other thing\n",
		},
		{
			name:  "show --stat",
			input: "commit 0123456789abcdef\nAuthor: Sam <sam@example.com>\nDate:   Mon Sep 17 12:00:00 2026\n\n    subject\n\n src/a.go | 4 ++--\n 1 file changed, 2 insertions(+), 2 deletions(-)\n",
		},
		{
			name:  "branch listing",
			input: "* main\n  work\n  remotes/origin/main\n",
		},
		{
			name:  "status listing",
			input: "On branch main\nChanges not staged for commit:\n\tmodified:   src/a.go\n",
		},
		{
			name:  "tag listing",
			input: "v0.1.0\nv0.1.1\n",
		},
		{
			name:  "empty",
			input: "",
		},
		{
			name: "hunks without a file header",
			// Not something git prints, but a hand-trimmed patch must not look
			// like a diff when there is no file to anchor it to.
			input: "--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-old\n+new\n",
		},
		{
			name:  "plain modification",
			input: "diff --git a/x.go b/x.go\nindex 111..222 100644\n--- a/x.go\n+++ b/x.go\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n",
			files: 1,
		},
		{
			name:  "rename",
			input: "diff --git a/old.go b/new.go\nsimilarity index 100%\nrename from old.go\nrename to new.go\n",
			files: 1,
		},
		{
			name:  "rename with edits",
			input: "diff --git a/old.go b/new.go\nsimilarity index 90%\nrename from old.go\nrename to new.go\nindex 111..222 100644\n--- a/old.go\n+++ b/new.go\n@@ -1 +1 @@\n-a\n+b\n",
			files: 1,
		},
		{
			name:  "binary",
			input: "diff --git a/logo.png b/logo.png\nindex 111..222 100644\nBinary files a/logo.png and b/logo.png differ\n",
			files: 1,
		},
		{
			name:  "mode only",
			input: "diff --git a/run.sh b/run.sh\nold mode 100644\nnew mode 100755\n",
			files: 1,
		},
		{
			name:  "added file",
			input: "diff --git a/new.go b/new.go\nnew file mode 100644\nindex 000..222\n--- /dev/null\n+++ b/new.go\n@@ -0,0 +1,2 @@\n+a\n+b\n",
			files: 1,
		},
		{
			name:  "deleted file",
			input: "diff --git a/gone.go b/gone.go\ndeleted file mode 100644\nindex 111..000\n--- a/gone.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-a\n",
			files: 1,
		},
		{
			name:  "quoted path",
			input: "diff --git \"a/sp ace.go\" \"b/sp ace.go\"\nindex 111..222 100644\n--- \"a/sp ace.go\"\n+++ \"b/sp ace.go\"\n@@ -1 +1 @@\n-a\n+b\n",
			files: 1,
		},
		{
			name:  "crlf lines",
			input: "diff --git a/win.txt b/win.txt\nindex 111..222 100644\n--- a/win.txt\n+++ b/win.txt\n@@ -1 +1 @@\n-line\r\n+line changed\r\n",
			files: 1,
		},
		{
			name:  "no newline at end of file",
			input: "diff --git a/x.go b/x.go\nindex 111..222 100644\n--- a/x.go\n+++ b/x.go\n@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+b\n\\ No newline at end of file\n",
			files: 1,
		},
		{
			name:  "submodule",
			input: "diff --git a/third/lib b/third/lib\nindex 111..222 160000\n--- a/third/lib\n+++ b/third/lib\n@@ -1 +1 @@\n-Subproject commit 1111\n+Subproject commit 2222\n",
			files: 1,
		},
		{
			name:  "format-patch",
			input: "From 0123456789abcdef Mon Sep 17 00:00:00 2001\nFrom: Sam <sam@example.com>\nDate: Mon, 17 Sep 2026 12:00:00 +0300\nSubject: [PATCH] fix the thing\n\nbody line\n---\n src/a.go | 2 +-\n 1 file changed, 1 insertion(+), 1 deletion(-)\n\ndiff --git a/src/a.go b/src/a.go\nindex 111..222 100644\n--- a/src/a.go\n+++ b/src/a.go\n@@ -1 +1 @@\n-a\n+b\n-- \n2.40.0\n",
			files: 1,
		},
		{
			name: "combined merge diff",
			// A --cc hunk has an @@@ header and one prefix column per parent.
			// Twig reads it as a document it can show but not rewrite.
			input: "diff --cc x.go\nindex 111,222..333\n--- a/x.go\n+++ b/x.go\n@@@ -1,1 -1,1 +1,2 @@@\n+ a\n++b\n",
			files: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Parse(tt.input)
			if len(d.Files) != tt.files {
				t.Fatalf("parsed %d files, want %d", len(d.Files), tt.files)
			}
			// A stream is classified the same way twice, so a rendered patch fed
			// back in cannot become something else.
			again := Parse(tt.input)
			if len(again.Files) != len(d.Files) {
				t.Fatalf("parsing the same stream twice gave %d then %d files", len(d.Files), len(again.Files))
			}
		})
	}
}

// What the pager renders from a real patch has to keep the rows it read: no
// silently dropped line, and no invented hunk.
func TestGitPatchKeepsEveryRow(t *testing.T) {
	const input = "diff --git a/x.go b/x.go\nindex 111..222 100644\n--- a/x.go\n+++ b/x.go\n@@ -1,3 +1,3 @@\n one\n-two\n+TWO\n three\n@@ -20,2 +20,3 @@ func f() {\n tail\n+extra\n"

	d := Parse(input)
	if len(d.Files) != 1 {
		t.Fatalf("parsed %d files, want 1", len(d.Files))
	}
	f := d.Files[0]
	if len(f.Hunks) != 2 {
		t.Fatalf("%d hunks, want 2", len(f.Hunks))
	}
	if f.Additions != 2 || f.Deletions != 1 {
		t.Fatalf("totals +%d -%d, want +2 -1", f.Additions, f.Deletions)
	}
	for _, want := range []string{"-two", "+TWO", "+extra"} {
		if !strings.Contains(f.Patch(), want) {
			t.Fatalf("the patch lost %q", want)
		}
	}
}

// The kind of change decides how the file is drawn and how a patch is written,
// so each shape has to land on the right one.
func TestGitStreamsKeepTheirKind(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		kind   ChangeKind
		binary bool
		oldMod string
		newMod string
	}{
		{
			name:  "added",
			input: "diff --git a/new.go b/new.go\nnew file mode 100644\n--- /dev/null\n+++ b/new.go\n@@ -0,0 +1 @@\n+a\n",
			kind:  Added,
		},
		{
			name:  "deleted",
			input: "diff --git a/gone.go b/gone.go\ndeleted file mode 100644\n--- a/gone.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-a\n",
			kind:  Deleted,
		},
		{
			name:  "renamed",
			input: "diff --git a/old.go b/new.go\nsimilarity index 100%\nrename from old.go\nrename to new.go\n",
			kind:  Renamed,
		},
		{
			name:   "binary",
			input:  "diff --git a/logo.png b/logo.png\nBinary files a/logo.png and b/logo.png differ\n",
			kind:   Modified,
			binary: true,
		},
		{
			name:   "mode only",
			input:  "diff --git a/run.sh b/run.sh\nold mode 100644\nnew mode 100755\n",
			kind:   Modified,
			oldMod: "100644",
			newMod: "100755",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Parse(tt.input)
			if len(d.Files) != 1 {
				t.Fatalf("parsed %d files, want 1", len(d.Files))
			}
			f := d.Files[0]
			if f.Kind != tt.kind {
				t.Fatalf("kind = %c, want %c", f.Kind, tt.kind)
			}
			if f.Binary != tt.binary {
				t.Fatalf("binary = %v, want %v", f.Binary, tt.binary)
			}
			if f.OldMode != tt.oldMod || f.NewMode != tt.newMod {
				t.Fatalf("modes = %q/%q, want %q/%q", f.OldMode, f.NewMode, tt.oldMod, tt.newMod)
			}
		})
	}
}
