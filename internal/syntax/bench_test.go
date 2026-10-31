package syntax

import (
	"os"
	"strings"
	"testing"
)

// loadFile returns a fixture split into lines.
func loadFile(tb testing.TB, path string) []string {
	tb.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("%s: %v", path, err)
	}
	return strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
}

// BenchmarkLine tokenises one ordinary Go line, the unit the render path pays
// for.
func BenchmarkLine(b *testing.B) {
	lang := goLang
	src := "func handler(w http.ResponseWriter, r *http.Request) error {"
	buf := make([]Span, 0, 64)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		buf, _ = lang.Line(buf[:0], src, 0)
	}
	_ = buf
}

// BenchmarkViewport tokenises the fifty lines a screen shows, carrying state
// the way a diff does.
func BenchmarkViewport(b *testing.B) {
	lang := goLang
	lines := loadFile(b, "testdata/sample.go")
	buf := make([]Span, 0, 64)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		var st State
		for _, line := range lines {
			buf, st = lang.Line(buf[:0], line, st)
		}
	}
}

// BenchmarkIndex builds the index of a large diff, which is the one-off cost of
// opening a file.
func BenchmarkIndex(b *testing.B) {
	lang := goLang
	lines := loadFile(b, "testdata/sample.go")
	const rows = 20_000
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		bld := NewBuilder(lang)
		for i := range rows {
			line := lines[i%len(lines)]
			bld.Context(line, i+1, i+1)
		}
		_ = bld.Index()
	}
}

// BenchmarkDetect measures the per-file language lookup, which runs once per
// diff.
func BenchmarkDetect(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if Detect("internal/syntax/syntax.go", "") == nil {
			b.Fatal("no language")
		}
	}
}
