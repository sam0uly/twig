package diff

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// corpusPair is one before/after pair the invariant checks run over.
type corpusPair struct {
	path     string
	old, new string
}

// repoPairs builds diff inputs from the repository's own sources: every Go file
// plus deterministic mutations of it. Real hunks without an external corpus, so
// the invariant checks see hundreds of them.
func repoPairs(t *testing.T) []corpusPair {
	t.Helper()
	root := filepath.Join("..", "..")

	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".crush", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".go") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	sort.Strings(files)

	var out []corpusPair
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil || len(b) == 0 || len(b) > 200_000 {
			continue
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			continue
		}
		for _, variant := range mutations(string(b)) {
			if variant != string(b) {
				out = append(out, corpusPair{path: rel, old: string(b), new: variant})
			}
		}
	}
	return out
}

// mutations are the edits every corpus file gets: a word change, a number
// change, a whitespace refold and an appended comment.
func mutations(src string) []string {
	return []string{
		replaceOnce(src, "func ", "fn "),
		replaceOnce(src, "1", "7"),
		replaceOnce(src, "\n\t", "\n    "),
		src + "\n// appended by the corpus\n",
	}
}

func replaceOnce(src, old, new string) string {
	if i := strings.Index(src, old); i >= 0 {
		return src[:i] + new + src[i+len(old):]
	}
	return src
}

// TestCorpusInvariants asserts the properties that must hold for every input:
// span geometry and pairing shape. These are properties, not samples, so a
// counterexample is a bug rather than a judgement call.
func TestCorpusInvariants(t *testing.T) {
	pairs := repoPairs(t)
	if len(pairs) < 40 {
		t.Skipf("corpus too small (%d pairs)", len(pairs))
	}
	t.Logf("corpus: %d pairs", len(pairs))

	for _, p := range pairs {
		f := Generate(p.path, Modified, p.old, p.new, GenerateOptions{Context: 3})
		checkSpanGeometry(t, p.path, f)
		checkPairingShape(t, p.path, f)
	}
}

// checkSpanGeometry asserts every highlight is inside its line and cannot split
// a rune, and that only changed rows carry one.
func checkSpanGeometry(t *testing.T, path string, f *File) {
	t.Helper()
	for i := 0; i < f.NLines(); i++ {
		l, ok := f.LineAt(i)
		if !ok {
			break
		}
		words := l.WordRanges()
		if len(words) > 0 && l.Kind != Deletion && l.Kind != Addition {
			t.Fatalf("%s row %d: %d spans on a %d-kind row", path, i, len(words), l.Kind)
		}
		for j, s := range words {
			if s[0] < 0 || s[1] > len(l.Text) || s[0] >= s[1] {
				t.Fatalf("%s row %d span %v out of bounds for %q", path, i, s, l.Text)
			}
			if j > 0 && words[j-1][1] > s[0] {
				t.Fatalf("%s row %d spans overlap or are unordered: %v", path, i, words)
			}
			if !utf8.RuneStart(l.Text[s[0]]) {
				t.Fatalf("%s row %d span %v starts mid-rune in %q", path, i, s, l.Text)
			}
			if s[1] < len(l.Text) && !utf8.RuneStart(l.Text[s[1]]) {
				t.Fatalf("%s row %d span %v ends mid-rune in %q", path, i, s, l.Text)
			}
		}
	}
}

// checkPairingShape asserts the pairing is a partial bijection that keeps order,
// never pairs below the floor, and mirrors when the two sides are swapped.
func checkPairingShape(t *testing.T, path string, f *File) {
	t.Helper()
	for _, h := range f.Hunks {
		var dels, adds []int
		for i := range h.Lines {
			switch h.Lines[i].Kind {
			case Deletion:
				dels = append(dels, i)
			case Addition:
				adds = append(adds, i)
			}
		}
		if len(dels) == 0 || len(adds) == 0 {
			continue
		}

		blocks := changeBlocks(h.Lines)
		if len(blocks) == 0 {
			t.Fatalf("%s: changed rows but no block", path)
		}
		var pairs [][2]int
		for _, b := range blocks {
			pairs = append(pairs, pairRows(h.Lines, b.dels, b.adds)...)
		}
		seenDel := make(map[int]bool, len(pairs))
		seenAdd := make(map[int]bool, len(pairs))
		prevDel, prevAdd := -1, -1
		for _, p := range pairs {
			if seenDel[p[0]] || seenAdd[p[1]] {
				t.Fatalf("%s: row paired twice: %v", path, pairs)
			}
			seenDel[p[0]], seenAdd[p[1]] = true, true
			if len(blocks) == 1 && (p[0] <= prevDel || p[1] <= prevAdd) {
				t.Fatalf("%s: pairing lost order: %v", path, pairs)
			}
			prevDel, prevAdd = p[0], p[1]

			in := make(interner, 16)
			score := bagSimilarity(
				wordBag(h.Lines[p[0]].Text[1:], in),
				wordBag(h.Lines[p[1]].Text[1:], in),
			)
			if score < homologyFloor {
				t.Fatalf("%s: paired rows below the floor (%.2f): %q / %q",
					path, score, h.Lines[p[0]].Text, h.Lines[p[1]].Text)
			}
		}

		if len(pairs) == 0 {
			continue
		}
		var mirror [][2]int
		for _, b := range changeBlocks(h.Lines) {
			mirror = append(mirror, pairRows(h.Lines, b.adds, b.dels)...)
		}
		if len(mirror) != len(pairs) {
			t.Fatalf("%s: %d pairs one way, %d the other", path, len(pairs), len(mirror))
		}
		for i, p := range pairs {
			m := mirror[len(mirror)-1-i]
			if m[0] != p[1] || m[1] != p[0] {
				t.Fatalf("%s: pairing is not symmetric: %v vs mirror %v", path, pairs, mirror)
			}
		}
	}
}

// TestCorpusIsDeterministic pins that the same input always produces the same
// rows and spans.
func TestCorpusIsDeterministic(t *testing.T) {
	for _, p := range repoPairs(t) {
		a := dumpRows(Generate(p.path, Modified, p.old, p.new, GenerateOptions{Context: 3}))
		b := dumpRows(Generate(p.path, Modified, p.old, p.new, GenerateOptions{Context: 3}))
		if a != b {
			t.Fatalf("%s: output differs between runs", p.path)
		}
	}
}

// dumpRows renders a file's rows, line numbers and spans as text, which is what
// the golden fixtures compare.
func dumpRows(f *File) string {
	var b strings.Builder
	for i := 0; i < f.NLines(); i++ {
		l, ok := f.LineAt(i)
		if !ok {
			break
		}
		b.WriteString(l.Text)
		b.WriteString("|")
		if len(l.WordRanges()) > 0 {
			b.WriteString(sprintSpans(l.WordRanges()))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// sprintSpans renders spans as the golden fixtures record them.
func sprintSpans(spans []Span) string {
	var b strings.Builder
	b.WriteString("[")
	for i, s := range spans {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(itoa(s[0]))
		b.WriteString(":")
		b.WriteString(itoa(s[1]))
	}
	b.WriteString("]")
	return b.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
