package syntax

import "testing"

// indexLines tokenises lines as the context side of one file.
func indexLines(t *testing.T, lang *Language, lines []string) *Index {
	t.Helper()
	b := NewBuilder(lang)
	for i, l := range lines {
		b.Context(l, i+1, i+1)
	}
	return b.Index()
}

// hasKeyword reports whether a keyword is among the tokens. It is how these
// tests ask "was this line scanned by the host": a keyword is the one class an
// embedded language adds that the markup table never gives, so a line the host
// scanned has none however the host chose to colour it.
func hasKeyword(toks []tok) bool {
	for _, v := range toks {
		if v.class == Keyword {
			return true
		}
	}
	return false
}

// TestMarkdownFence checks that a fenced block is scanned by the table its
// info tag names, and that the host takes the closer back.
func TestMarkdownFence(t *testing.T) {
	lines := []string{
		"# Title",
		"```go",
		"func main() {",
		"\tprintln(\"hi\")",
		"}",
		"```",
		"after",
	}
	idx := indexLines(t, Detect("notes.md", ""), lines)

	if got := spansOf(idx.Old(3), lines[2]); !equalTokens(got, []tok{
		{Keyword, "func"}, {Function, "main"}, {Punct, "("}, {Punct, ")"}, {Punct, "{"},
	}) {
		t.Fatalf("fence body = %v", got)
	}
	if got := spansOf(idx.Old(4), lines[3]); !equalTokens(got, []tok{
		{Function, "println"}, {Punct, "("}, {String, `"hi"`}, {Punct, ")"},
	}) {
		t.Fatalf("fence body 2 = %v", got)
	}
	if got := spansOf(idx.Old(6), lines[5]); !equalTokens(got, []tok{{String, "```"}}) {
		t.Fatalf("fence closer = %v", got)
	}
	if got := spansOf(idx.Old(7), lines[6]); len(got) != 0 {
		t.Fatalf("after the fence = %v, want plain", got)
	}
}

// TestMarkdownFenceUnknownTag leaves the body plain.
func TestMarkdownFenceUnknownTag(t *testing.T) {
	lines := []string{
		"```nope",
		"plain body",
		"```",
	}
	idx := indexLines(t, Detect("notes.md", ""), lines)
	if got := spansOf(idx.Old(2), lines[1]); len(got) != 0 {
		t.Fatalf("unknown tag body = %v, want plain", got)
	}
}

// TestMarkdownFenceUnterminated keeps scanning the body until the end of the
// file.
func TestMarkdownFenceUnterminated(t *testing.T) {
	lines := []string{
		"```go",
		"var x = 1",
	}
	idx := indexLines(t, Detect("notes.md", ""), lines)
	if got := spansOf(idx.Old(2), lines[1]); !equalTokens(got, []tok{
		{Keyword, "var"}, {Operator, "="}, {Number, "1"},
	}) {
		t.Fatalf("unterminated fence body = %v", got)
	}
}

// TestMarkdownFenceClosesWithMoreOfTheSameChar and a fence opened with tildes.
func TestMarkdownFenceClosesWithMoreOfTheSameChar(t *testing.T) {
	lines := []string{
		"```go",
		"x := 1",
		"````",
		"after",
		"~~~python",
		"def f():",
		"~~~",
	}
	idx := indexLines(t, Detect("notes.md", ""), lines)
	if got := spansOf(idx.Old(2), lines[1]); !equalTokens(got, []tok{
		{Operator, ":="}, {Number, "1"},
	}) {
		t.Fatalf("backtick fence body = %v", got)
	}
	if got := spansOf(idx.Old(4), lines[3]); len(got) != 0 {
		t.Fatalf("after the taller close = %v, want plain", got)
	}
	if got := spansOf(idx.Old(6), lines[5]); !equalTokens(got, []tok{
		{Keyword, "def"}, {Function, "f"}, {Punct, "("}, {Punct, ")"}, {Operator, ":"},
	}) {
		t.Fatalf("tilde fence body = %v", got)
	}
}

// TestPHPInHTML checks that a .php page scans its HTML as HTML and its
// <?php ... ?> islands as PHP.
func TestPHPInHTML(t *testing.T) {
	lines := []string{
		"<!doctype html>",
		"<html>",
		"<body>",
		"<h1>Hi</h1>",
		"<?php",
		"$x = 1;",
		"echo \"hi\";",
		"?>",
		"</body>",
		"</html>",
	}
	idx := indexLines(t, Detect("page.php", ""), lines)

	if got := spansOf(idx.Old(4), lines[3]); !equalTokens(got, []tok{{Tag, "h1"}, {Tag, "h1"}}) {
		t.Fatalf("php page markup = %v", got)
	}
	if got := spansOf(idx.Old(6), lines[5]); !equalTokens(got, []tok{
		{Constant, "$x"}, {Operator, "="}, {Number, "1"}, {Punct, ";"},
	}) {
		t.Fatalf("php code = %v", got)
	}
	if got := spansOf(idx.Old(7), lines[6]); !equalTokens(got, []tok{
		{Keyword, "echo"}, {String, `"hi"`}, {Punct, ";"},
	}) {
		t.Fatalf("php code 2 = %v", got)
	}
	if got := spansOf(idx.Old(9), lines[8]); !equalTokens(got, []tok{{Tag, "body"}}) {
		t.Fatalf("php page markup after = %v", got)
	}
}

// TestVueAttributeExpression scans the value of a :attr as JavaScript instead
// of one opaque string.
func TestVueAttributeExpression(t *testing.T) {
	lines := []string{`<div :class="a ? 'b' : 'c'">`}
	idx := indexLines(t, Detect("comp.vue", ""), lines)
	if got := spansOf(idx.Old(1), lines[0]); !equalTokens(got, []tok{
		{Tag, "div"},
		{Operator, ":"},
		{Key, "class"},
		{Operator, "="},
		{Punct, `"`},
		{Operator, "?"},
		{Key, `'b'`},
		{Operator, ":"},
		{String, `'c'`},
		{Punct, `"`},
	}) {
		t.Fatalf("vue attribute expression = %v", got)
	}
}

// TestVueMustache scans {{ ... }} in element text.
func TestVueMustache(t *testing.T) {
	lines := []string{`<p>{{ count + 1 }}</p>`}
	idx := indexLines(t, Detect("comp.vue", ""), lines)
	if got := spansOf(idx.Old(1), lines[0]); !equalTokens(got, []tok{
		{Tag, "p"}, {Punct, "{{"}, {Operator, "+"}, {Number, "1"}, {Punct, "}}"}, {Tag, "p"},
	}) {
		t.Fatalf("vue mustache = %v", got)
	}
}

// TestMustacheInAttributeValue scans {{ ... }} inside an attribute value of
// plain HTML.
func TestMustacheInAttributeValue(t *testing.T) {
	lines := []string{`<a title="{{ msg }}">`}
	idx := indexLines(t, Detect("x.html", ""), lines)
	if got := spansOf(idx.Old(1), lines[0]); !equalTokens(got, []tok{
		{Tag, "a"},
		{Key, "title"},
		{Operator, "="},
		{String, `"`},
		{Punct, "{{"},
		{Punct, "}}"},
		{String, `"`},
	}) {
		t.Fatalf("mustache in attribute = %v", got)
	}
}

// TestVueAttributesOffInHTML keeps plain HTML from scanning :attr values as
// JavaScript.
func TestVueAttributesOffInHTML(t *testing.T) {
	lines := []string{`<div :class="a + b">`}
	idx := indexLines(t, Detect("x.html", ""), lines)
	if got := spansOf(idx.Old(1), lines[0]); !equalTokens(got, []tok{
		{Tag, "div"}, {Operator, ":"}, {Key, "class"}, {Operator, "="}, {String, `"a + b"`},
	}) {
		t.Fatalf("html attribute value = %v", got)
	}
}

// TestVueMultiLineExpressionFallsBack leaves the host to scan the line when
// the expression's closer is not on the same line.
func TestVueMultiLineExpressionFallsBack(t *testing.T) {
	lines := []string{
		`<div :class="a ?`,
		`'b' : 'c'">`,
	}
	idx := indexLines(t, Detect("comp.vue", ""), lines)
	if got := spansOf(idx.Old(1), lines[0]); !equalTokens(got, []tok{
		{Tag, "div"}, {Operator, ":"}, {Key, "class"}, {Operator, "="}, {String, `"a ?`},
	}) {
		t.Fatalf("multi-line expression opener = %v", got)
	}
	if got := spansOf(idx.Old(2), lines[1]); !equalTokens(got, []tok{
		{String, `'b'`}, {Operator, ":"}, {String, `'c'`}, {String, `">`},
	}) {
		t.Fatalf("multi-line expression rest = %v", got)
	}
}

// TestScriptBlockClosedOnItsOpeningLine keeps a one-line script element from
// opening a region that runs to the end of the file, which is the shape of
// almost every external script tag on the web.
func TestScriptBlockClosedOnItsOpeningLine(t *testing.T) {
	lines := []string{
		`<script src="app.js"></script>`,
		"<p>after</p>",
		"const x = 1",
	}
	idx := indexLines(t, Detect("x.html", ""), lines)
	if got := spansOf(idx.Old(2), lines[1]); !equalTokens(got, []tok{{Tag, "p"}, {Tag, "p"}}) {
		t.Fatalf("line after the block = %v, want HTML", got)
	}
	if got := spansOf(idx.Old(3), lines[2]); hasKeyword(got) {
		t.Fatalf("line after the block = %v, want the HTML table, not JS", got)
	}
}

// TestStyleBlockClosedOnItsOpeningLine covers the same shape for a style
// element.
func TestStyleBlockClosedOnItsOpeningLine(t *testing.T) {
	lines := []string{
		`<style>.a { color: red }</style>`,
		"<p>after</p>",
	}
	idx := indexLines(t, Detect("x.html", ""), lines)
	if got := spansOf(idx.Old(2), lines[1]); !equalTokens(got, []tok{{Tag, "p"}, {Tag, "p"}}) {
		t.Fatalf("line after the block = %v, want HTML", got)
	}
}

// TestEmbedCloseIgnoresCase closes a region the way HTML does, on any case.
func TestEmbedCloseIgnoresCase(t *testing.T) {
	lines := []string{
		"<style>",
		".a { color: red }",
		"</STYLE>",
		"<p>after</p>",
		"const x = 1",
	}
	idx := indexLines(t, Detect("x.html", ""), lines)
	if got := spansOf(idx.Old(4), lines[3]); !equalTokens(got, []tok{{Tag, "p"}, {Tag, "p"}}) {
		t.Fatalf("line after the block = %v, want HTML", got)
	}
	if got := spansOf(idx.Old(5), lines[4]); hasKeyword(got) {
		t.Fatalf("line after the block 2 = %v, want the HTML table, not CSS", got)
	}
}

// TestScriptingIsNotAScriptTag keeps a longer word that starts with the opener
// from opening a region.
func TestScriptingIsNotAScriptTag(t *testing.T) {
	lines := []string{
		"<scripting>",
		"const x = 1",
	}
	idx := indexLines(t, Detect("x.html", ""), lines)
	if got := spansOf(idx.Old(2), lines[1]); hasKeyword(got) {
		t.Fatalf("line after <scripting> = %v, want the HTML table, not JS", got)
	}
}

// TestContextKeepsEmbedOnBothSides scans a changed line inside a region the
// context line above it opened. The two sides of that context line agree, so
// one run of tokens is reused, but the region still has to reach both sides:
// the line after it is a deletion and an addition with no context line between.
func TestContextKeepsEmbedOnBothSides(t *testing.T) {
	b := NewBuilder(Detect("x.html", ""))
	b.Context("<script>", 1, 1)
	b.Old("const a = 1;", 2)
	b.New("const b = 2;", 2)
	idx := b.Index()

	if got := spansOf(idx.Old(2), "const a = 1;"); !equalTokens(got, []tok{
		{Keyword, "const"}, {Operator, "="}, {Number, "1"}, {Punct, ";"},
	}) {
		t.Fatalf("old side = %v", got)
	}
	if got := spansOf(idx.New(2), "const b = 2;"); !equalTokens(got, []tok{
		{Keyword, "const"}, {Operator, "="}, {Number, "2"}, {Punct, ";"},
	}) {
		t.Fatalf("new side = %v, want the script region the context line opened", got)
	}
}

// TestSelfClosingScriptOpensNoRegion keeps `<script/>` and its spacings from
// opening a body that no `</script` ever closes, which would scan the rest of
// the file in the inner language.
func TestSelfClosingScriptOpensNoRegion(t *testing.T) {
	openers := []string{
		`<script/>`,
		`<script />`,
		`<script src="a.js" />`,
	}
	for _, opener := range openers {
		t.Run(opener, func(t *testing.T) {
			lines := []string{
				opener,
				"<p>after</p>",
				"const x = 1",
			}
			idx := indexLines(t, Detect("x.html", ""), lines)
			if got := spansOf(idx.Old(2), lines[1]); !equalTokens(got, []tok{{Tag, "p"}, {Tag, "p"}}) {
				t.Fatalf("line after the opener = %v, want HTML", got)
			}
			if got := spansOf(idx.Old(3), lines[2]); hasKeyword(got) {
				t.Fatalf("line after the opener 2 = %v, want the HTML table, not JS", got)
			}
		})
	}
}
