package syntax

import "testing"

// TestMarkupProseIsPlain keeps the markup tables free of an element word list:
// position already names a tag and an attribute, so a word list could only leak
// into the prose between tags, where "a", "table" and "title" are words.
func TestMarkupProseIsPlain(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
		src  string
		want []tok
	}{
		{"html one word article", htmlLang, "<p>Hello</p>", []tok{
			{Tag, "p"}, {Tag, "p"},
		}},
		{"html tag names in prose", htmlLang, "<p>table small title</p>", []tok{
			{Tag, "p"}, {Tag, "p"},
		}},
		{"html attributes by position", htmlLang, `<a href="/x" data-id='7'>`, []tok{
			{Tag, "a"},
			{Key, "href"},
			{Operator, "="},
			{String, `"/x"`},
			{Key, "data-id"},
			{Operator, "="},
			{String, `'7'`},
		}},
		{"html prose line", htmlLang, "a table small title", nil},
		{"html closing tag", htmlLang, "</body>", []tok{{Tag, "body"}}},
		{"vue prose", vueLang, "<template><p>small title</p></template>", []tok{
			{Tag, "template"}, {Tag, "p"}, {Tag, "p"}, {Tag, "template"},
		}},
		{"vue attribute", vueLang, `<div :class="c">`, []tok{
			{Tag, "div"},
			{Operator, ":"},
			{Key, "class"},
			{Operator, "="},
			{Punct, `"`},
			{Punct, `"`},
		}},
		{"xml prose", xmlLang, "<a>table</a>", []tok{{Tag, "a"}, {Tag, "a"}}},
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

// TestHaskellCharLiteral covers 'x' as a string and leaves the other uses of
// the quote byte alone: it closes neither foldl' nor a Template Haskell quote.
func TestHaskellCharLiteral(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []tok
	}{
		{"char", `let c = 'x'`, []tok{
			{Keyword, "let"}, {Operator, "="}, {String, "'x'"},
		}},
		{"escape", `c = '\n'`, []tok{{Operator, "="}, {String, `'\n'`}}},
		{"escaped quote", `c = '\''`, []tok{{Operator, "="}, {String, `'\''`}}},
		{"quote in a name", `f = foldl' (+) 0`, []tok{
			{Operator, "="}, {Punct, "("}, {Operator, "+"}, {Punct, ")"}, {Number, "0"},
		}},
		{"template haskell quote", `y = 'Name`, []tok{{Operator, "="}}},
		{"two runes", `z = 'ab'`, []tok{{Operator, "="}}},
		{"apostrophe in a string", `main = putStrLn "it's"`, []tok{
			{Operator, "="}, {String, `"it's"`},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := haskellLang.Line(nil, c.src, 0)
			if words := tokens(c.src, got); !equalTokens(words, c.want) {
				t.Fatalf("tokens = %v, want %v", words, c.want)
			}
		})
	}
}

// TestDollarSpecialVars covers the shell and awk special parameters, which are
// a sigil and a punctuation byte rather than a name, and keeps the named
// variables and the braced and command forms working.
func TestDollarSpecialVars(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
		src  string
		want []tok
	}{
		{"shell parameters", shellLang, `echo $1 $? $@ $# $$ $! $* $-`, []tok{
			{Constant, "$1"},
			{Constant, "$?"},
			{Constant, "$@"},
			{Constant, "$#"},
			{Constant, "$$"},
			{Constant, "$!"},
			{Constant, "$*"},
			{Constant, "$-"},
		}},
		{"shell named variable", shellLang, `x=$HOME`, []tok{
			{Operator, "="}, {Constant, "$HOME"},
		}},
		{"shell braced and command", shellLang, `v=${V} w=$(date)`, []tok{
			{Operator, "="},
			{Punct, "{"},
			{Punct, "}"},
			{Operator, "="},
			{Punct, "("},
			{Punct, ")"},
		}},
		{"awk field", awkLang, `print $1, $NF`, []tok{
			{Keyword, "print"}, {Constant, "$1"}, {Punct, ","}, {Constant, "$NF"},
		}},
		{"tcl", tclLang, `set x $1`, []tok{
			{Keyword, "set"}, {Constant, "$1"},
		}},
		{"powershell", powershellLang, `$?`, []tok{{Constant, "$?"}}},
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

// TestVerilogSizedLiteral covers the width-and-base literal and the unbased
// form, which the decimal scan alone would split at the apostrophe.
func TestVerilogSizedLiteral(t *testing.T) {
	cases := []struct {
		src  string
		want []tok
	}{
		{`x = 8'hFF;`, []tok{{Operator, "="}, {Number, "8'hFF"}, {Punct, ";"}}},
		{`x = 4'b1010;`, []tok{{Operator, "="}, {Number, "4'b1010"}, {Punct, ";"}}},
		{`x = 16'd42;`, []tok{{Operator, "="}, {Number, "16'd42"}, {Punct, ";"}}},
		{`x = 12'sd7;`, []tok{{Operator, "="}, {Number, "12'sd7"}, {Punct, ";"}}},
		{`x = 'bz;`, []tok{{Operator, "="}, {Number, "'bz"}, {Punct, ";"}}},
		{`x = '1;`, []tok{{Operator, "="}, {Number, "'1"}, {Punct, ";"}}},
		{`wire [7:0] y;`, []tok{
			{Keyword, "wire"},
			{Punct, "["},
			{Number, "7"},
			{Operator, ":"},
			{Number, "0"},
			{Punct, "]"},
			{Punct, ";"},
		}},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, _ := verilogLang.Line(nil, c.src, 0)
			if words := tokens(c.src, got); !equalTokens(words, c.want) {
				t.Fatalf("tokens = %v, want %v", words, c.want)
			}
		})
	}
}

// TestErlangBaseAndChar covers the hash-spelled base literal, which the decimal
// scan would end at the hash, and the $ character literal, which it did not see
// at all.
func TestErlangBaseAndChar(t *testing.T) {
	cases := []struct {
		src  string
		want []tok
	}{
		{`X = 16#FF.`, []tok{{Operator, "="}, {Number, "16#FF"}, {Punct, "."}}},
		{`Y = 2#1010,`, []tok{{Operator, "="}, {Number, "2#1010"}, {Punct, ","}}},
		{`C = $a,`, []tok{{Operator, "="}, {Number, "$a"}, {Punct, ","}}},
		{`D = $\n.`, []tok{{Operator, "="}, {Number, `$\n`}, {Punct, "."}}},
		{`E = $\\`, []tok{{Operator, "="}, {Number, `$\\`}}},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, _ := erlangLang.Line(nil, c.src, 0)
			if words := tokens(c.src, got); !equalTokens(words, c.want) {
				t.Fatalf("tokens = %v, want %v", words, c.want)
			}
		})
	}
}

// TestGoHexFloat covers the p exponent that only a hexadecimal float has, and
// pins the ordinary floats, the underscores and the integer bases it must not
// disturb.
func TestGoHexFloat(t *testing.T) {
	cases := []struct {
		src  string
		want []tok
	}{
		{`x := 0x1p-2`, []tok{{Operator, ":="}, {Number, "0x1p-2"}}},
		{`x := 0x1.fp+3`, []tok{{Operator, ":="}, {Number, "0x1.fp+3"}}},
		{`x := 0x1P4`, []tok{{Operator, ":="}, {Number, "0x1P4"}}},
		{`x := 1.5e-3`, []tok{{Operator, ":="}, {Number, "1.5e-3"}}},
		{`x := 1_000`, []tok{{Operator, ":="}, {Number, "1_000"}}},
		{`x := 0xFF`, []tok{{Operator, ":="}, {Number, "0xFF"}}},
		{`x := 0x1e5`, []tok{{Operator, ":="}, {Number, "0x1e5"}}},
		{`x := 0b1010`, []tok{{Operator, ":="}, {Number, "0b1010"}}},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, _ := goLang.Line(nil, c.src, 0)
			if words := tokens(c.src, got); !equalTokens(words, c.want) {
				t.Fatalf("tokens = %v, want %v", words, c.want)
			}
		})
	}
}

// TestCSharpInterpolatedString covers the $ prefix as part of the literal, in
// both orders and with the verbatim @, and leaves the verbatim and raw strings
// that were already right alone.
func TestCSharpInterpolatedString(t *testing.T) {
	cases := []struct {
		src  string
		want []tok
	}{
		{`var s = $"a{b}c";`, []tok{
			{Keyword, "var"}, {Operator, "="}, {String, `$"a{b}c"`}, {Punct, ";"},
		}},
		{`var s = $"a\"b";`, []tok{
			{Keyword, "var"},
			{Operator, "="},
			{String, `$"a`},
			{Escape, `\"`},
			{String, `b"`},
			{Punct, ";"},
		}},
		{`var s = $@"a""b";`, []tok{
			{Keyword, "var"}, {Operator, "="}, {String, `$@"a""b"`}, {Punct, ";"},
		}},
		{`var s = @$"x";`, []tok{
			{Keyword, "var"}, {Operator, "="}, {String, `@$"x"`}, {Punct, ";"},
		}},
		{`var s = @"x";`, []tok{
			{Keyword, "var"}, {Operator, "="}, {String, `@"x"`}, {Punct, ";"},
		}},
		{`var s = "x";`, []tok{
			{Keyword, "var"}, {Operator, "="}, {String, `"x"`}, {Punct, ";"},
		}},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, _ := csharpLang.Line(nil, c.src, 0)
			if words := tokens(c.src, got); !equalTokens(words, c.want) {
				t.Fatalf("tokens = %v, want %v", words, c.want)
			}
		})
	}
}

// TestCSharpInterpolatedAcrossLines walks the verbatim interpolated string and
// the raw string across a line boundary: the prefix opens the body, and the
// state the opener returns has to keep the next line inside the literal.
func TestCSharpInterpolatedAcrossLines(t *testing.T) {
	cases := []struct {
		name string
		raw  RawKind
		line string
		want []tok
	}{
		{"verbatim open", RawCSharpVerbatim, `var v = $@"a`, []tok{
			{Keyword, "var"}, {Operator, "="}, {String, `$@"a`},
		}},
		{"verbatim close", 0, `b";`, []tok{{String, `b"`}, {Punct, ";"}}},
		{"raw open", RawCSharpRaw, `var r = $"""`, []tok{
			{Keyword, "var"}, {Operator, "="}, {String, `"""`},
		}},
		{"raw body", 0, `a "b" c`, []tok{{String, `a "b" c`}}},
		{"raw close", 0, `""";`, []tok{{String, `"""`}, {Punct, ";"}}},
		{"plain verbatim open", RawCSharpVerbatim, `var v = @"a`, []tok{
			{Keyword, "var"}, {Operator, "="}, {String, `@"a`},
		}},
		{"plain verbatim close", 0, `b";`, []tok{{String, `b"`}, {Punct, ";"}}},
	}
	var st State
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, next := csharpLang.Line(nil, c.line, st)
			if words := tokens(c.line, got); !equalTokens(words, c.want) {
				t.Fatalf("tokens = %v, want %v", words, c.want)
			}
			if c.raw != 0 && (!next.raw() || RawKind(next.rule()) != c.raw) {
				t.Fatalf("state = %d, not the expected opener", next)
			}
			st = next
		})
	}
}
