package syntax

import "testing"

// jsxLines scans a list of lines as TypeScript-with-JSX and returns the tokens
// of each, which is how a file is actually scanned: line by line, carrying the
// state.
func jsxLines(t *testing.T, lines []string) [][]tok {
	t.Helper()
	var st State
	var buf []Span
	out := make([][]tok, len(lines))
	for i, l := range lines {
		buf, st = typescriptJSX.Line(buf[:0], l, st)
		out[i] = tokens(l, buf)
	}
	if st.mode() != modeJS {
		t.Fatalf("left in JSX mode %d after %d lines", st.mode(), len(lines))
	}
	return out
}

func TestJSXTagAttributesAndChildren(t *testing.T) {
	got := jsxLines(t, []string{
		`const el = <div className="box">{a}</div>;`,
	})
	want := []tok{
		{Keyword, "const"},
		{Operator, "="},
		{Tag, "div"},
		{Key, "className"},
		{Operator, "="},
		{String, `"box"`},
		{Punct, "{"},
		{Punct, "}"},
		{Tag, "div"},
		{Punct, ";"},
	}
	if !equalTokens(got[0], want) {
		t.Fatalf("tokens = %v, want %v", got[0], want)
	}
}

func TestJSXTagSpansLines(t *testing.T) {
	// A tag whose attributes are on the next lines is the common shape, and the
	// rule must not need a ">" on the same line.
	got := jsxLines(t, []string{
		"return (",
		"  <Foo.Bar",
		`    onClick={go}`,
		"  >",
		"    text",
		"  </Foo.Bar>",
		");",
	})
	if !equalTokens(got[1], []tok{{Tag, "Foo.Bar"}}) {
		t.Fatalf("tag line = %v", got[1])
	}
	if !equalTokens(got[2], []tok{{Key, "onClick"}, {Operator, "="}, {Punct, "{"}, {Punct, "}"}}) {
		t.Fatalf("attributes = %v", got[2])
	}
	if !equalTokens(got[5], []tok{{Tag, "Foo.Bar"}}) {
		t.Fatalf("closing tag = %v", got[5])
	}
}

func TestJSXSelfClosingAndFragment(t *testing.T) {
	got := jsxLines(t, []string{`const a = <><Icon size={2} /></>;`})
	want := []tok{
		{Keyword, "const"},
		{Operator, "="},
		{Tag, "Icon"},
		{Key, "size"},
		{Operator, "="},
		{Punct, "{"},
		{Number, "2"},
		{Punct, "}"},
		{Punct, ";"},
	}
	if !equalTokens(got[0], want) {
		t.Fatalf("tokens = %v, want %v", got[0], want)
	}
}

// TestJSXLeavesJSTokensAlone is the guard that matters most: comparisons and
// generic parameter lists must not turn into tags.
func TestJSXLeavesJSTokensAlone(t *testing.T) {
	cases := []struct {
		src  string
		want []tok
	}{
		{`const smaller = a < b;`, []tok{
			{Keyword, "const"}, {Operator, "="}, {Operator, "<"}, {Punct, ";"},
		}},
		{`function f<T,>(x: T) { return x; }`, []tok{
			{Keyword, "function"},
			{Operator, "<"},
			{Punct, ","},
			{Operator, ">"},
			{Punct, "("},
			{Key, "x"},
			{Operator, ":"},
			{Punct, ")"},
			{Punct, "{"},
			{Keyword, "return"},
			{Punct, ";"},
			{Punct, "}"},
		}},
		{"if (a < b && c > d) { i += 1; }", nil},
	}
	for _, c := range cases {
		var st State
		got, st := typescriptJSX.Line(nil, c.src, st)
		words := tokens(c.src, got)
		if c.want != nil && !equalTokens(words, c.want) {
			t.Fatalf("%q = %v, want %v", c.src, words, c.want)
		}
		if st.mode() != modeJS {
			t.Fatalf("%q left JSX mode %d", c.src, st.mode())
		}
	}
}

// TestEmbeddedBlocks checks that a component's script and style bodies are
// scanned by the language their lang attribute names, and that the host wins at
// the closing tag.
func TestEmbeddedBlocks(t *testing.T) {
	lines := []string{
		"<template>",
		`  <div class="a">text</div>`,
		"</template>",
		`<script setup lang="ts">`,
		"const count: number = 1;",
		"// a comment",
		"</script>",
		`<style scoped lang="scss">`,
		".a { color: red; }",
		"</style>",
	}
	b := NewBuilder(Detect("page.vue", ""))
	for i, l := range lines {
		b.Context(l, i+1, i+1)
	}
	idx := b.Index()

	if got := spansOf(idx.Old(5), lines[4]); !equalTokens(got, []tok{
		{Keyword, "const"},
		{Key, "count"},
		{Operator, ":"},
		{Type, "number"},
		{Operator, "="},
		{Number, "1"},
		{Punct, ";"},
	}) {
		t.Fatalf("script body = %v", got)
	}
	if got := spansOf(idx.Old(6), lines[5]); !equalTokens(got, []tok{{Comment, "// a comment"}}) {
		t.Fatalf("script comment = %v", got)
	}
	if got := spansOf(idx.Old(9), lines[8]); !equalTokens(got, []tok{
		{Punct, "."}, {Punct, "{"}, {Key, "color"}, {Operator, ":"}, {Punct, ";"}, {Punct, "}"},
	}) {
		t.Fatalf("style body = %v", got)
	}
	// The closing tag is host text again, not JavaScript.
	if got := spansOf(idx.Old(7), lines[6]); !equalTokens(got, []tok{{Tag, "script"}}) {
		t.Fatalf("closing script = %v", got)
	}
}

// TestEmbedClosesOnTheHostLine makes sure an unclosed string inside the inner
// language cannot swallow the closing tag.
func TestEmbedClosesOnTheHostLine(t *testing.T) {
	lines := []string{
		"<script>",
		"const t = `a",
		"b</script>",
		"<p>after</p>",
	}
	b := NewBuilder(Detect("x.html", ""))
	for i, l := range lines {
		b.Context(l, i+1, i+1)
	}
	idx := b.Index()
	if got := spansOf(idx.Old(4), lines[3]); !equalTokens(got, []tok{{Tag, "p"}, {Tag, "p"}}) {
		t.Fatalf("line after the block = %v, want HTML again", got)
	}
}
