package diff

import (
	"math"
	"strings"
	"testing"
)

// TestBytesCountsSources pins that a paged diff charges its sources: that is
// what makes a cache bound meaningful when the rows themselves are four bytes
// each.
func TestBytesCountsSources(t *testing.T) {
	oldText := strings.Repeat("same line\n", 6000)
	newText := strings.ReplaceAll(oldText, "same line\n", "changed line\n")

	f := paged(t, oldText, newText, GenerateOptions{Context: 3})
	if got, want := f.Bytes() >= len(oldText)+len(newText), true; got != want {
		t.Fatalf("paged bytes %d should cover both sources (%d)", f.Bytes(), len(oldText)+len(newText))
	}

	grow := paged(t, oldText, newText+"tail\n", GenerateOptions{Context: 3})
	if grow.Bytes() <= f.Bytes() {
		t.Fatalf("more content did not grow the estimate: %d vs %d", grow.Bytes(), f.Bytes())
	}

	var nilFile *File
	if nilFile.Bytes() != 0 {
		t.Fatalf("nil file bytes = %d", nilFile.Bytes())
	}
}

// TestPagedBytesBelowResident compares the two layouts of the same diff: the
// packed descriptors must cost less than the rendered lines they replace.
func TestPagedBytesBelowResident(t *testing.T) {
	oldText := strings.Repeat("func handler() error { return nil }\n", 6000)
	newText := strings.ReplaceAll(oldText, "return nil", "return err")

	res := generate("f", Modified, oldText, newText, GenerateOptions{Context: 3}, math.MaxInt)
	pg := generate("f", Modified, oldText, newText, GenerateOptions{Context: 3}, 0)
	if res.Paged() || !pg.Paged() {
		t.Fatalf("fixtures not in the expected layouts: resident=%v paged=%v", res.Paged(), pg.Paged())
	}
	if pg.Bytes() >= res.Bytes() {
		t.Fatalf("paged bytes %d, want below resident %d", pg.Bytes(), res.Bytes())
	}
}

// TestSlimKeepsIdentity drops the payload but keeps what a listing row shows.
func TestSlimKeepsIdentity(t *testing.T) {
	f := Generate("dir/file.go", Renamed, "a\nb\n", "a\nc\n", GenerateOptions{Context: 3})
	f.OldPath = "dir/old.go"
	f.OldMode, f.NewMode = "100644", "100755"

	slim := f.Slim()
	if slim.Path() != f.Path() || slim.OldPath != f.OldPath {
		t.Fatalf("paths lost: %+v", slim)
	}
	if slim.Additions != f.Additions || slim.Deletions != f.Deletions || slim.Kind != f.Kind {
		t.Fatalf("counts lost: %+v", slim)
	}
	if slim.NLines() != 0 || slim.NHunks() != 0 {
		t.Fatalf("slim kept %d lines and %d hunks", slim.NLines(), slim.NHunks())
	}
	if slim.Bytes() >= f.Bytes() {
		t.Fatalf("slim bytes %d, want below %d", slim.Bytes(), f.Bytes())
	}
	if (*File)(nil).Slim() != nil {
		t.Fatal("slim of a nil file should stay nil")
	}
}
