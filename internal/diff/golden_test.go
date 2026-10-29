package diff

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files under testdata/golden")

// goldenCase is one fixture whose full row and span layout is pinned.
type goldenCase struct {
	name string
	path string
	old  string
	new  string
}

// goldenCases is the curated set: one per behaviour that could regress. Each
// case's expected rows live in testdata/golden/<name>.golden, which records the
// highlighted spans and their total byte count, so a noisier diff fails the
// test instead of quietly highlighting more.
func goldenCases() []goldenCase {
	proseOld := "Built with the Charm stack: [Bubble Tea v2], [Bubbles v2],\n" +
		"[ultraviolet] and [harmonica], wrapped in a [cobra] CLI.\n"
	proseNew := "Built on the Charm stack: [Bubble Tea v2], [Bubbles v2],\n" +
		"[ultraviolet] and [harmonica], wrapped in a [cobra] CLI.\n"

	return []goldenCase{
		{"identifier", "a.go", "x := foo(bar)\n", "x := foo(baz)\n"},
		{"string-word", "a.go", "printSuccess(\"added %s\", name)\n", "printSuccess(\"addedo %s\", name)\n"},
		{"operator", "a.go", "x := a + b\n", "x := a - b\n"},
		{"parens", "a.go", "x := a + b\n", "x := (a + b)\n"},
		{"comma", "a.go", "call(a, b)\n", "call(a b)\n"},
		{"reindent", "a.go", "func f() {\n\tx := 1\n}\n", "func f() {\n    x := 1\n}\n"},
		{"comment-text", "a.go", "x := 1 // one\n", "x := 1 // two\n"},
		{"comment-added", "a.go", "x := 1\n", "x := 1 // note\n"},
		{"prose-rewrite", "README.md", proseOld, proseNew},
		{"multiline-rewrite", "a.go",
			"func f() {\n\ta()\n\tb()\n\tc()\n}\n",
			"func f() {\n\tc()\n\tb()\n\ta()\n}\n"},
		{"crlf", "a.go", "x := 1\r\ny := 2\r\n", "x := 9\r\ny := 2\r\n"},
		{"cjk", "a.go", "s := \"日本語\"\n", "s := \"日本後\"\n"},
		{"json", "a.json", "{\"a\": 1, \"b\": \"x y\"}\n", "{\"a\": 2, \"b\": \"x  y\"}\n"},
		{"css", "a.css", "a{color:red}\n", "a{color:blue}\n"},
		{"added-file", "new.go", "", "a := 1\nb := 2\n"},
		{"deleted-file", "gone.go", "a := 1\nb := 2\n", ""},
		{"no-final-newline", "a.txt", "a\nb", "a\nc"},
		{"blank-line-inserted", "a.go", "x := 1\ny := 2\n", "x := 1\n\ny := 2\n"},
	}
}

// TestGoldenRows pins the rendered rows and highlights of the curated cases.
// Run `go test ./internal/diff -update` after an intentional change.
func TestGoldenRows(t *testing.T) {
	dir := filepath.Join("testdata", "golden")
	if *updateGolden {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			f := Generate(c.path, Modified, c.old, c.new, GenerateOptions{Context: 3})
			got := goldenDump(f)

			path := filepath.Join(dir, c.name+".golden")
			if *updateGolden {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v (run with -update)", path, err)
			}
			if got != string(want) {
				t.Fatalf("%s does not match:\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
			}
		})
	}
}

// goldenDump is the comparison form: one line per row, plus the total number of
// highlighted bytes as the noise metric.
func goldenDump(f *File) string {
	var b strings.Builder
	fmt.Fprintf(&b, "rows=%d hunks=%d\n", f.NLines(), f.NHunks())
	total := 0
	for i := 0; i < f.NLines(); i++ {
		l, ok := f.LineAt(i)
		if !ok {
			break
		}
		spans := l.WordRanges()
		for _, s := range spans {
			total += s[1] - s[0]
		}
		fmt.Fprintf(&b, "row %02d kind=%d old=%d new=%d text=%s spans=%s\n",
			i, l.Kind, l.Old, l.New, strconv.Quote(l.Text), sprintSpans(spans))
	}
	fmt.Fprintf(&b, "words=%d\n", total)
	return b.String()
}
