package diff

import (
	"math/rand"
	"strings"
	"testing"
)

func check(t *testing.T, it int, oldText, newText string) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("panic it=%d old=%q new=%q: %v", it, oldText, newText, p)
		}
	}()
	f := Generate("f", Modified, oldText, newText, GenerateOptions{Context: 3})

	oldN, newN := lineCount(oldText), lineCount(newText)
	for i := range f.NHunks() {
		h, ok := f.HunkAt(i)
		if !ok {
			t.Fatalf("it=%d: hunk %d missing", it, i)
		}
		var olds, news int
		for _, l := range h.Lines {
			if l.Old < 0 || l.Old > oldN {
				t.Fatalf("it=%d: old line %d out of range (old file has %d lines)", it, l.Old, oldN)
			}
			if l.New < 0 || l.New > newN {
				t.Fatalf("it=%d: new line %d out of range (new file has %d lines)", it, l.New, newN)
			}
			if l.Old > 0 {
				olds++
			}
			if l.New > 0 {
				news++
			}
		}
		if olds == 0 && news == 0 {
			t.Fatalf("it=%d: empty hunk %q", it, h.Header)
		}
	}
	if oldText != newText && len(f.Hunks) == 0 {
		t.Fatalf("it=%d: differing texts produced no hunks", it)
	}
	if f.Additions+f.Deletions == 0 && oldText != newText {
		t.Fatalf("it=%d: differing texts produced no changed lines", it)
	}
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return len(strings.Split(strings.TrimSuffix(s, "\n"), "\n"))
}

func TestFuzzBig(t *testing.T) {
	alphabet := []string{"a", "b", "", "x", "  y", "if err != nil {", "z", "a", "b"}
	r := rand.New(rand.NewSource(7))
	for it := 0; it < 3000; it++ {
		gen := func(max int) string {
			n := r.Intn(max)
			var b strings.Builder
			for i := 0; i < n; i++ {
				b.WriteString(alphabet[r.Intn(len(alphabet))])
				b.WriteString("\n")
			}
			return b.String()
		}
		check(t, it, gen(600), gen(600))
	}
}

func TestFuzzHuge(t *testing.T) {
	alphabet := []string{"a", "b", "", "x", "y"}
	r := rand.New(rand.NewSource(9))
	for it := 0; it < 20; it++ {
		var a, b strings.Builder
		for i := 0; i < 5000; i++ {
			a.WriteString(alphabet[r.Intn(len(alphabet))])
			a.WriteString("\n")
			b.WriteString(alphabet[r.Intn(len(alphabet))])
			b.WriteString("\n")
		}
		check(t, it, a.String(), b.String())
	}
}
