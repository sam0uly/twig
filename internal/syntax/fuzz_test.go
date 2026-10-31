package syntax

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzLine feeds one line to every language table three times in a row, so the
// state that crosses a line boundary is exercised too, and asserts the
// invariants the renderer relies on: spans inside the line, ordered and
// non-overlapping, paintable classes, and no span boundary splitting a rune.
func FuzzLine(f *testing.F) {
	f.Add("(</") // a JSX closing tag split across a line boundary
	f.Add("</")
	f.Add("<")
	f.Add("<>")
	f.Add("</div>")
	f.Add(`<div class="x" data-y={1}>`)
	f.Add("const x = a < b ? c : d")
	f.Add("func (t *Type) M() error {")
	f.Add("/* an open comment")
	f.Add("/* a comment */")
	f.Add(`"an unterminated string`)
	f.Add("#![a raw attribute]")
	f.Add(`r#"raw body"#`)
	f.Add("<<EOF")
	f.Add("<<-'EOF'")
	f.Add("=> { [a, b]: c }")
	f.Add("1_000.5e-3")
	f.Add("\t// indented comment")
	f.Add("x$y#z")
	f.Add("")

	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > 4096 {
			t.Skip() // past the index cap no real diff line reaches
		}
		valid := utf8.ValidString(src)
		for _, lang := range languages {
			var st State
			buf := make([]Span, 0, 64)
			for range 3 {
				buf, st = lang.Line(buf[:0], src, st)
				checkSpans(t, src, buf, valid)
			}
		}
	})
}

// fuzzBuilderPaths names one file per language table that has embeds: a fenced
// code block, an HTML/Vue block, a PHP island, a heredoc, a raw string, or a
// nestable comment. Detect maps each path back to its table.
var fuzzBuilderPaths = []string{
	"f.md",   // markdown fences, and a fence that embeds HTML
	"f.html", // <script> and <style> regions
	"f.vue",  // component blocks and template expressions
	"f.php",  // <?php islands in an HTML page
	"f.sh",   // heredocs
	"f.rb",   // heredocs and atoms
	"f.rs",   // raw strings and nested block comments
	"f.cs",   // verbatim and interpolated strings
	"f.cpp",  // raw strings and nested block comments
}

// FuzzBuilder feeds a sequence of source lines to a full Builder for every
// language with embed machinery, mixing Context lines with Old/New pairs and
// one-sided lines the way a unified diff presents them. Line numbers start at
// one and only ever increase, so the per-side scanner state, the old/new merge
// in Context, and the divergent-state fallback all run.
func FuzzBuilder(f *testing.F) {
	// Inputs that have broken this code before, or come from the shapes that
	// did: the old JSX panic, script/style openers and near-openers, unclosed
	// fences and heredocs, PHP islands, an unterminated block comment, a line
	// that is only a backslash, and a line past the index's 4095-byte cap.
	for _, seed := range []string{
		"(</",
		`<script src="app.js"></script>`,
		"<script>\nvar x = 1;",
		"<SCRIPT>",
		"<scripting>",
		"```go\nfunc main() {}",
		"```html\n<script>\nalert(1)",
		"<<EOT\nbody line with no terminator",
		"<?php",
		"<?php\necho 1;",
		"/*",
		"/* an open comment",
		"foo \\",
		strings.Repeat("a", 5000),
		// A few more region openers, cheap to keep.
		"<style>",
		"<!--\nstill open",
		`r#"raw body`,
		`R"x(body`,
		`@"verbatim`,
		"{{",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > 8192 {
			t.Skip() // keep the worst case per execution bounded
		}
		lines := strings.Split(src, "\n")
		if len(lines) > 32 {
			lines = lines[:32]
		}
		for i, line := range lines {
			if len(line) > 4096 {
				lines[i] = line[:4096]
			}
		}
		for _, path := range fuzzBuilderPaths {
			lang := Detect(path, "")
			if lang == nil {
				t.Fatalf("Detect(%q) returned no language", path)
			}
			b := NewBuilder(lang)
			if b == nil {
				t.Fatalf("NewBuilder(%q) returned nil", lang.Name)
			}
			idx := b.Index()
			if idx == nil {
				t.Fatalf("Builder.Index for %q returned nil", lang.Name)
			}
			oldLine, newLine := 1, 1
			for i, line := range lines {
				switch fuzzMode(src, i) {
				case 0:
					b.Context(line, oldLine, newLine)
					checkTokens(t, lang, "old", oldLine, line, idx.Old(oldLine))
					checkTokens(t, lang, "new", newLine, line, idx.New(newLine))
					oldLine++
					newLine++
				case 1:
					b.Old(line, oldLine)
					checkTokens(t, lang, "old", oldLine, line, idx.Old(oldLine))
					oldLine++
					b.New(line, newLine)
					checkTokens(t, lang, "new", newLine, line, idx.New(newLine))
					newLine++
				case 2:
					b.New(line, newLine)
					checkTokens(t, lang, "new", newLine, line, idx.New(newLine))
					newLine++
				default:
					b.Old(line, oldLine)
					checkTokens(t, lang, "old", oldLine, line, idx.Old(oldLine))
					oldLine++
				}
			}
		}
	})
}

// fuzzMode picks which builder call a line exercises, deterministically from
// the input and the line index: context, an old/new pair, an addition, or a
// deletion. One-sided lines make the two scanner states drift apart, which is
// what Context's merge has to notice.
func fuzzMode(src string, i int) int {
	h := uint32(2166136261)
	for k := 0; k < len(src) && k < 64; k++ {
		h = (h ^ uint32(src[k])) * 16777619
	}
	h = (h ^ uint32(i)*2654435761) * 16777619
	return int(h>>24) & 3
}

// checkTokens asserts the invariants the renderer relies on for the packed
// tokens of one side of one line: every span inside the line, a paintable
// class, non-decreasing starts, and no overlap with the previous span. It also
// unpacks each word twice, which must be stable, since the render path decodes
// the same packed token more than once.
func checkTokens(t *testing.T, lang *Language, side string, line int, src string, toks []uint32) {
	t.Helper()
	n := int32(len(src))
	var prevStart, prevEnd int32
	for i, w := range toks {
		start, end, class := UnpackToken(w)
		s2, e2, c2 := UnpackToken(w)
		if start != s2 || end != e2 || class != c2 {
			t.Fatalf("%s %s line %d: token %d unpacked as (%d,%d,%d) then (%d,%d,%d) in %q",
				lang.Name, side, line, i, start, end, class, s2, e2, c2, src)
		}
		if start < 0 || end < start || end > n {
			t.Fatalf("%s %s line %d: token %d span (%d,%d) outside line of %d bytes in %q",
				lang.Name, side, line, i, start, end, n, src)
		}
		if class >= ClassCount {
			t.Fatalf("%s %s line %d: token %d span (%d,%d) has class %d >= %d in %q",
				lang.Name, side, line, i, start, end, class, ClassCount, src)
		}
		if i > 0 && start < prevStart {
			t.Fatalf("%s %s line %d: token %d span (%d,%d) starts before previous start %d in %q",
				lang.Name, side, line, i, start, end, prevStart, src)
		}
		if i > 0 && start < prevEnd {
			t.Fatalf("%s %s line %d: token %d span (%d,%d) overlaps previous span ending at %d in %q",
				lang.Name, side, line, i, start, end, prevEnd, src)
		}
		prevStart, prevEnd = start, end
	}
}

// checkSpans asserts the invariants a Span must satisfy for the renderer to
// slice the line by its byte offsets.
func checkSpans(t *testing.T, src string, spans []Span, valid bool) {
	t.Helper()
	n := int32(len(src))
	var end int32
	for _, s := range spans {
		if s.Start < 0 || s.End > n || s.Start >= s.End {
			t.Fatalf("span %v outside line %q", s, src)
		}
		if s.Start < end {
			t.Fatalf("span %v overlaps the previous span in line %q", s, src)
		}
		if s.Class >= ClassCount {
			t.Fatalf("span %v has class %d >= %d in line %q", s, s.Class, ClassCount, src)
		}
		if valid {
			// A span boundary inside a multi-byte rune would paint half a
			// character when the renderer slices by byte offset.
			if int(s.Start) < len(src) && !utf8.RuneStart(src[s.Start]) {
				t.Fatalf("span %v starts inside a rune in line %q", s, src)
			}
			if int(s.End) < len(src) && !utf8.RuneStart(src[s.End]) {
				t.Fatalf("span %v ends inside a rune in line %q", s, src)
			}
		}
		end = s.End
	}
}
