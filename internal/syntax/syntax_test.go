package syntax

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tok is one expected token: its class and the text it covers.
type tok struct {
	class Class
	text  string
}

// tokens renders spans as classes and substrings, which is what a reader of the
// test wants to see when a case fails.
func tokens(src string, spans []Span) []tok {
	out := make([]tok, 0, len(spans))
	for _, s := range spans {
		out = append(out, tok{s.Class, src[s.Start:s.End]})
	}
	return out
}

// equalTokens compares two token lists.
func equalTokens(a, b []tok) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLineClasses(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
		src  string
		st   State
		want []tok
		end  State
	}{
		{
			name: "go call and comment",
			lang: goLang,
			src:  "func main() { // hi",
			want: []tok{
				{Keyword, "func"},
				{Function, "main"},
				{Punct, "("},
				{Punct, ")"},
				{Punct, "{"},
				{Comment, "// hi"},
			},
		},
		{
			name: "go operator string number",
			lang: goLang,
			src:  `x := "a\"b" + 1`,
			want: []tok{
				{Operator, ":="},
				{String, `"a`},
				{Escape, `\"`},
				{String, `b"`},
				{Operator, "+"},
				{Number, "1"},
			},
		},
		{
			name: "go type and constant",
			lang: goLang,
			src:  "var n int32 = MaxValue",
			want: []tok{
				{Keyword, "var"},
				{Type, "int32"},
				{Operator, "="},
				{Type, "MaxValue"},
			},
		},
		{
			name: "go raw string opens and closes",
			lang: goLang,
			src:  "s := `raw",
			want: []tok{
				{Operator, ":="},
				{String, "`raw"},
			},
			end: stringState(2, false),
		},
		{
			name: "go raw string resumes",
			lang: goLang,
			src:  "x` + 1",
			st:   stringState(2, false),
			want: []tok{
				{String, "x`"},
				{Operator, "+"},
				{Number, "1"},
			},
		},
		{
			name: "go block comment opens",
			lang: goLang,
			src:  "/* a",
			want: []tok{{Comment, "/* a"}},
			end:  stateBlock,
		},
		{
			name: "go block comment closes and continues",
			lang: goLang,
			src:  " b */ x",
			st:   stateBlock,
			want: []tok{
				{Comment, " b */"},
			},
		},
		{
			name: "go block comment on one line",
			lang: goLang,
			src:  "a /* b */ c",
			want: []tok{
				{Comment, "/* b */"},
			},
		},
		{
			name: "slash inside a string is not a comment",
			lang: goLang,
			src:  `p := "a // b"`,
			want: []tok{
				{Operator, ":="},
				{String, `"a // b"`},
			},
		},
		{
			name: "quote inside a comment is not a string",
			lang: goLang,
			src:  `// don't " x`,
			want: []tok{{Comment, `// don't " x`}},
		},
		{
			name: "python def and hash comment",
			lang: pythonLang,
			src:  "def f(x):  # note",
			want: []tok{
				{Keyword, "def"},
				{Function, "f"},
				{Punct, "("},
				{Punct, ")"},
				{Operator, ":"},
				{Comment, "# note"},
			},
		},
		{
			name: "python docstring opens",
			lang: pythonLang,
			src:  `    """Doc`,
			want: []tok{{String, `"""Doc`}},
			end:  stringState(0, true),
		},
		{
			name: "python docstring resumes and closes",
			lang: pythonLang,
			src:  "    body",
			st:   stringState(0, true),
			want: []tok{{String, "    body"}},
			end:  stringState(0, true),
		},
		{
			name: "python constants",
			lang: pythonLang,
			src:  "if x is None:",
			want: []tok{
				{Keyword, "if"},
				{Keyword, "is"},
				{Constant, "None"},
				{Operator, ":"},
			},
		},
		{
			name: "rust lifetime is not a string",
			lang: rustLang,
			src:  `let x: &'a str = "s";`,
			want: []tok{
				{Keyword, "let"},
				{Operator, ":"},
				{Operator, "&"},
				{Type, "str"},
				{Operator, "="},
				{String, `"s"`},
				{Punct, ";"},
			},
		},
		{
			name: "javascript template literal spans lines",
			lang: javascriptLang,
			src:  "const a = `x ${b}",
			want: []tok{
				{Keyword, "const"},
				{Operator, "="},
				{String, "`x ${b}"},
			},
			end: stringState(2, false),
		},
		{
			name: "json keys and values",
			lang: jsonLang,
			src:  `{"a": 1, "b": true}`,
			want: []tok{
				{Punct, "{"},
				{Key, `"a"`},
				{Operator, ":"},
				{Number, "1"},
				{Punct, ","},
				{Key, `"b"`},
				{Operator, ":"},
				{Boolean, "true"},
				{Punct, "}"},
			},
		},
		{
			name: "shell string then comment",
			lang: shellLang,
			src:  `echo "$PATH" # no`,
			want: []tok{
				{String, `"$PATH"`},
				{Comment, "# no"},
			},
		},
		{
			name: "hash inside a word is not a comment",
			lang: yamlLang,
			src:  "key: a#b",
			want: []tok{
				{Key, "key"},
				{Operator, ":"},
			},
		},
		{
			name: "html comment",
			lang: htmlLang,
			src:  "<!-- note -->",
			want: []tok{{Comment, "<!-- note -->"}},
		},
		{
			name: "html tag and attribute",
			lang: htmlLang,
			src:  `<div class="x">`,
			want: []tok{
				{Tag, "div"},
				{Key, "class"},
				{Operator, "="},
				{String, `"x"`},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var dst []Span
			got, end := c.lang.Line(dst, c.src, c.st)
			if words := tokens(c.src, got); !equalTokens(words, c.want) {
				t.Fatalf("tokens = %v, want %v", words, c.want)
			}
			if end != c.end {
				t.Fatalf("state = %d, want %d", end, c.end)
			}
		})
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		path string
		line string
		want *Language
	}{
		{"main.go", "", goLang},
		{"src/lib.rs", "", rustLang},
		{"x/y/test.py", "", pythonLang},
		{"Makefile", "", shellLang},
		{"deploy/Dockerfile", "", shellLang},
		{"app.tsx", "", typescriptJSX},
		{"style.css", "", cssLang},
		{"conf.TOML", "", tomlLang},
		{"script", "#!/usr/bin/env python3", pythonLang},
		{"script", "#!/bin/sh", shellLang},
		{"notes.txt", "", nil},
		{"", "", nil},
		{"mod.d", "", dLang},
		{"core.nim", "", nimLang},
		{"greet.cr", "", crystalLang},
		{"main.m", "", objcLang},
		{"file.mm", "", objcLang},
		{"kernel.cu", "", cudaLang},
		{"grid.f90", "", fortranLang},
		{"paper.tex", "", latexLang},
		{"alu.sv", "", verilogLang},
		{"report.awk", "", awkLang},
		{"init.tcl", "", tclLang},
		{"run.bat", "", batchLang},
		{"rc.vim", "", vimLang},
		{"lib.scm", "", lispLang},
		{"main.fs", "", fsharpLang},
		{"Main.purs", "", purescriptLang},
		{"lib.raku", "", rakuLang},
		{"nginx.conf", "", nginxLang},
	}
	for _, c := range cases {
		if got := Detect(c.path, c.line); got != c.want {
			t.Errorf("Detect(%q, %q) = %v, want %v", c.path, c.line, name(got), name(c.want))
		}
	}
}

// name is a language's name for error messages.
func name(l *Language) string {
	if l == nil {
		return "nil"
	}
	return l.Name
}

// TestTableBudget keeps support from quietly becoming bloat: every language
// costs bytes in the binary, so the count and the word list are asserted rather
// than assumed.
func TestTableBudget(t *testing.T) {
	// 64 is the measured count after the 2026 language pass; the cap now has
	// no headroom, so a new table must trim or raise this line deliberately.
	if n := Languages(); n > 64 {
		t.Errorf("%d languages, budget is 64", n)
	}
	// 3300 is the 3000-word budget raised only as far as the measured 3294
	// reserved words after adding D, Nim, Crystal, Objective-C, CUDA,
	// Fortran, LaTeX, Verilog, awk, Tcl, batch, Vim, Lisp, F#, PureScript,
	// Raku and nginx.
	if n := Words(); n > 3300 {
		t.Errorf("%d reserved words, budget is 3300", n)
	}
}

// TestFixtures checks every fixture against the invariants that make a span
// safe to render: inside the line, non-empty, and never overlapping.
func TestFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/*")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, path := range files {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			// testdata/fuzz holds the saved fuzz corpus, not fixtures.
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		lang := Detect(path, "")
		if lang == nil {
			t.Fatalf("%s: no language detected", path)
		}
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		var st State
		var spans []Span
		for i, line := range lines {
			spans, st = lang.Line(spans[:0], line, st)
			var last int32
			for _, s := range spans {
				if s.Start < 0 || s.End > int32(len(line)) || s.Start >= s.End {
					t.Fatalf("%s:%d: span %v outside a %d byte line", path, i+1, s, len(line))
				}
				if s.Start < last {
					t.Fatalf("%s:%d: spans overlap at %v", path, i+1, s)
				}
				if s.Class == Plain || s.Class >= ClassCount {
					t.Fatalf("%s:%d: class %d is not paintable", path, i+1, s.Class)
				}
				last = s.End
			}
		}
	}
}

// TestStateSurvivesEveryLine feeds each fixture one line at a time and requires
// the state to close by the end of the file, which is what a lost state byte
// would break.
func TestStateSurvivesEveryLine(t *testing.T) {
	for _, path := range []string{"testdata/sample.go", "testdata/sample.py"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		lang := Detect(path, "")
		var st State
		var spans []Span
		for i, line := range strings.Split(string(data), "\n") {
			spans, st = lang.Line(spans[:0], line, st)
			if s := st; s != 0 && i == len(strings.Split(string(data), "\n"))-1 {
				t.Fatalf("%s: state %d still open at end of file", path, s)
			}
		}
	}
}

// TestNoAllocs is the promise the render path relies on: tokenising a line
// costs no allocation at all.
func TestNoAllocs(t *testing.T) {
	lang := goLang
	src := "func handler(w http.ResponseWriter, r *http.Request) error {"
	buf := make([]Span, 0, 64)
	st := State(0)
	if n := testing.AllocsPerRun(100, func() {
		var s State
		buf, s = lang.Line(buf[:0], src, st)
		_ = s
	}); n != 0 {
		t.Fatalf("%v allocations per line, want 0", n)
	}
	if len(buf) == 0 {
		t.Fatal("no tokens")
	}
}

// TestLogicalClasses covers the classes that carry meaning rather than grammar:
// a key is the name before a separator, a doc comment documents, an escape is
// part of a literal, a markup attribute is a name and a tag is a keyword.
func TestLogicalClasses(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
		src  string
		want []tok
	}{
		{
			name: "yaml mapping key",
			lang: yamlLang,
			src:  "  runs-on: ubuntu-latest",
			want: []tok{{Key, "runs-on"}, {Operator, ":"}},
		},
		{
			name: "toml key uses equals",
			lang: tomlLang,
			src:  `name = "twig"`,
			want: []tok{{Key, "name"}, {Operator, "="}, {String, `"twig"`}},
		},
		{
			name: "css property",
			lang: cssLang,
			src:  "  color: var(--fg);",
			want: []tok{
				{Key, "color"},
				{Operator, ":"},
				{Punct, "("},
				{Operator, "--"},
				{Punct, ")"},
				{Punct, ";"},
			},
		},
		{
			name: "markup tag and attributes",
			lang: htmlLang,
			src:  `<a href="/x" data-id='7'>`,
			want: []tok{
				{Tag, "a"},
				{Key, "href"},
				{Operator, "="},
				{String, `"/x"`},
				{Key, "data-id"},
				{Operator, "="},
				{String, `'7'`},
			},
		},
		{
			name: "closing tag",
			lang: htmlLang,
			src:  "</body>",
			want: []tok{{Tag, "body"}},
		},
		{
			name: "shell variable",
			lang: shellLang,
			src:  `echo "$HOME"`,
			want: []tok{{String, `"$HOME"`}},
		},
		{
			name: "shell bare variable",
			lang: shellLang,
			src:  "cp $SRC $DST",
			want: []tok{
				{Constant, "$SRC"},
				{Constant, "$DST"},
			},
		},
		{
			name: "rust doc comment",
			lang: rustLang,
			src:  "/// Reads the file.",
			want: []tok{{Doc, "/// Reads the file."}},
		},
		{
			name: "rust plain comment",
			lang: rustLang,
			src:  "// a note",
			want: []tok{{Comment, "// a note"}},
		},
		{
			name: "doc block",
			lang: rustLang,
			src:  "/** documented */",
			want: []tok{{Doc, "/** documented */"}},
		},
		{
			name: "empty block is not a doc",
			lang: rustLang,
			src:  "/**/",
			want: []tok{{Comment, "/**/"}},
		},
		{
			name: "escape sequence",
			lang: goLang,
			src:  `s := "a\tb\nc"`,
			want: []tok{
				{Operator, ":="},
				{String, `"a`},
				{Escape, `\t`},
				{String, `b`},
				{Escape, `\n`},
				{String, `c"`},
			},
		},
		{
			name: "scope is not a key",
			lang: rustLang,
			src:  "std::io::read",
			want: []tok{{Operator, "::"}, {Operator, "::"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := c.lang.Line(nil, c.src, 0)
			if words := tokens(c.src, got); !equalTokens(words, c.want) {
				t.Fatalf("tokens = %v, want %v", words, c.want)
			}
		})
	}
}

// TestMoreLanguages covers the tables added after the first batch, one
// behaviour each: a heading, a key with an equals sign, a shell-style variable,
// a quoted atom, a markup tag and an assembly mnemonic.
func TestMoreLanguages(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
		src  string
		want []tok
		end  State
	}{
		{
			name: "markdown heading",
			lang: markdownLang,
			src:  "## Why it is fast",
			want: []tok{{Keyword, "## Why it is fast"}},
		},
		{
			name: "markdown inline code",
			lang: markdownLang,
			src:  "press `s` to stage",
			want: []tok{{String, "`s`"}},
		},
		{
			name: "markdown fence opens a block",
			lang: markdownLang,
			src:  "```go",
			want: []tok{{String, "```go"}},
			end:  stringState(0, true),
		},
		{
			name: "terraform attribute",
			lang: hclLang,
			src:  `  ami = "ami-1234"`,
			want: []tok{{Key, "ami"}, {Operator, "="}, {String, `"ami-1234"`}},
		},
		{
			name: "powershell variable",
			lang: powershellLang,
			src:  "$name = Get-Item",
			want: []tok{
				{Constant, "$name"},
				{Operator, "="},
				{Type, "Get-Item"},
			},
		},
		{
			name: "elixir atom and heredoc",
			lang: elixirLang,
			src:  `:ok`,
			want: []tok{{Constant, ":ok"}},
		},
		{
			name: "haskell guard",
			lang: haskellLang,
			src:  "  | n < 0 = \"negative\"",
			want: []tok{
				{Operator, "|"},
				{Operator, "<"},
				{Number, "0"},
				{Operator, "="},
				{String, `"negative"`},
			},
		},
		{
			name: "elm doc block",
			lang: elmLang,
			src:  "{-| A documented module. -}",
			want: []tok{{Doc, "{-| A documented module. -}"}},
		},
		{
			name: "elm type annotation",
			lang: elmLang,
			src:  "lookup : String -> Maybe Int",
			want: []tok{
				{Operator, ":"},
				{Type, "String"},
				{Operator, "->"},
				{Type, "Maybe"},
				{Type, "Int"},
			},
		},
		{
			name: "elm constructor and value",
			lang: elmLang,
			src:  "Just value",
			want: []tok{{Type, "Just"}},
		},
		{
			name: "elm nothing",
			lang: elmLang,
			src:  "Nothing",
			want: []tok{{Constant, "Nothing"}},
		},
		{
			name: "zig doc comment",
			lang: zigLang,
			src:  "/// Reads the file.",
			want: []tok{{Doc, "/// Reads the file."}},
		},
		{
			name: "xml tag and attribute",
			lang: xmlLang,
			src:  `<svg width="10">`,
			want: []tok{{Tag, "svg"}, {Key, "width"}, {Operator, "="}, {String, `"10"`}},
		},
		{
			name: "protobuf field",
			lang: protoLang,
			src:  "  string name = 1;",
			want: []tok{
				{Type, "string"},
				{Key, "name"},
				{Operator, "="},
				{Number, "1"},
				{Punct, ";"},
			},
		},
		{
			name: "assembly mnemonic",
			lang: asmLang,
			src:  "  mov eax, 1",
			want: []tok{{Keyword, "mov"}, {Type, "eax"}, {Punct, ","}, {Number, "1"}},
		},
		{
			name: "solidity pragma",
			lang: solidityLang,
			src:  "pragma solidity ^0.8.0;",
			want: []tok{
				{Keyword, "pragma"},
				{Operator, "^"},
				{Number, "0.8"},
				{Number, ".0"},
				{Punct, ";"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, end := c.lang.Line(nil, c.src, 0)
			if words := tokens(c.src, got); !equalTokens(words, c.want) {
				t.Fatalf("tokens = %v, want %v", words, c.want)
			}
			if end != c.end {
				t.Fatalf("state = %d, want %d", end, c.end)
			}
		})
	}
}

// TestSelfClosingTagCloses pins "/>" closing the tag state at the end of its
// line, so the next line is text and not more attribute names.
func TestSelfClosingTagCloses(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
	}{
		{"html", htmlLang},
		{"vue", vueLang},
		{"svelte", vueLang},
		{"xml", xmlLang},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, st := c.lang.Line(nil, "<p>Hello<br/>", 0)
			if words := tokens("<p>Hello<br/>", got); !equalTokens(words, []tok{
				{Tag, "p"},
				{Tag, "br"},
			}) {
				t.Fatalf("tokens = %v", words)
			}
			if st != 0 {
				t.Fatalf("state = %d, want no open tag", st)
			}
			got, _ = c.lang.Line(nil, "world</p>", st)
			if words := tokens("world</p>", got); !equalTokens(words, []tok{{Tag, "p"}}) {
				t.Fatalf("next line tokens = %v, want plain text", words)
			}

			got, st = c.lang.Line(nil, `<img src="a.png" />`, 0)
			if words := tokens(`<img src="a.png" />`, got); !equalTokens(words, []tok{
				{Tag, "img"},
				{Key, "src"},
				{Operator, "="},
				{String, `"a.png"`},
			}) {
				t.Fatalf("empty element tokens = %v", words)
			}
			if st != 0 {
				t.Fatalf("empty element state = %d, want no open tag", st)
			}
		})
	}
}
