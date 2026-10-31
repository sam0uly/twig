package syntax

import "testing"

// lineTokens renders the tokens of one line of an index, which keeps the
// expected lists in this file independent of how a span is packed.
func lineTokens(x *Index, src string, line int) []tok {
	var out []tok
	for _, w := range x.New(line) {
		start, end, class := UnpackToken(w)
		out = append(out, tok{class, src[start:end]})
	}
	return out
}

// TestSpotChecks pins the classes on one representative line per language. The
// multi-line cases feed the whole sample to a Builder first, so a block
// comment, a heredoc or a raw string resumed from an earlier line is checked
// in the state a diff would see it.
func TestSpotChecks(t *testing.T) {
	cases := []struct {
		name  string
		lang  *Language
		lines []string
		want  [][]tok
	}{
		{
			name:  "c include",
			lang:  cLang,
			lines: []string{"#include <stdio.h>"},
			want: [][]tok{{
				{Keyword, "#"}, {Keyword, "include"}, {String, "<stdio.h>"},
			}},
		},
		{
			name:  "cpp include",
			lang:  cppLang,
			lines: []string{"#include <vector>"},
			want: [][]tok{{
				{Keyword, "#"}, {Keyword, "include"}, {String, "<vector>"},
			}},
		},
		{
			name:  "lua block comment",
			lang:  luaLang,
			lines: []string{"--[[ open", "body", " close ]]"},
			want: [][]tok{
				{{Comment, "--[[ open"}},
				{{Comment, "body"}},
				{{Comment, " close ]]"}},
			},
		},
		{
			name:  "julia block comment",
			lang:  juliaLang,
			lines: []string{"#= open", "body", " close =#"},
			want: [][]tok{
				{{Comment, "#= open"}},
				{{Comment, "body"}},
				{{Comment, " close =#"}},
			},
		},
		{
			name:  "nim block comment",
			lang:  nimLang,
			lines: []string{"#[ open", "body", " close ]#"},
			want: [][]tok{
				{{Comment, "#[ open"}},
				{{Comment, "body"}},
				{{Comment, " close ]#"}},
			},
		},
		{
			name:  "zig declaration",
			lang:  zigLang,
			lines: []string{"const x = 1;"},
			want: [][]tok{{
				{Keyword, "const"}, {Operator, "="}, {Number, "1"}, {Punct, ";"},
			}},
		},
		{
			name:  "zig multiline string",
			lang:  zigLang,
			lines: []string{`    \\hello world`},
			want:  [][]tok{{{String, `\\hello world`}}},
		},
		{
			name:  "nix indented string",
			lang:  nixLang,
			lines: []string{"let s = ''", "  body ${x}", "'';"},
			want: [][]tok{
				{{Keyword, "let"}, {Operator, "="}, {String, "''"}},
				{{String, "  body ${x}"}},
				{{String, "''"}, {Punct, ";"}},
			},
		},
		{
			name:  "hcl heredoc",
			lang:  hclLang,
			lines: []string{"value = <<EOT", "hello", "EOT"},
			want: [][]tok{
				{{Key, "value"}, {Operator, "="}, {Operator, "<<"}, {String, "EOT"}},
				{{String, "hello"}},
				{{String, "EOT"}},
			},
		},
		{
			name:  "csharp raw string",
			lang:  csharpLang,
			lines: []string{`var s = """`, "hello", `""";`},
			want: [][]tok{
				{{Keyword, "var"}, {Operator, "="}, {String, `"""`}},
				{{String, "hello"}},
				{{String, `"""`}, {Punct, ";"}},
			},
		},
		{
			name:  "csharp verbatim string",
			lang:  csharpLang,
			lines: []string{`var v = @"a`, `b";`},
			want: [][]tok{
				{{Keyword, "var"}, {Operator, "="}, {String, `@"a`}},
				{{String, `b"`}, {Punct, ";"}},
			},
		},
		{
			name:  "java text block",
			lang:  javaLang,
			lines: []string{`var s = """`, "hello", `""";`},
			want: [][]tok{
				{{Keyword, "var"}, {Operator, "="}, {String, `"""`}},
				{{String, "hello"}},
				{{String, `"""`}, {Punct, ";"}},
			},
		},
		{
			// '#' is a PHP attribute marker, not the line comment of python or
			// the block comment opener of nim, so no token is a Comment.
			name:  "php attribute",
			lang:  phpLang,
			lines: []string{"#[Attr]"},
			want: [][]tok{{
				{Punct, "["}, {Type, "Attr"}, {Punct, "]"},
			}},
		},
		{
			// The element opens and closes with no body, so the prose line is
			// not an attribute name and carries no Key token.
			name:  "html self closing tag",
			lang:  htmlLang,
			lines: []string{"<br/>", "plain prose here"},
			want: [][]tok{
				{{Tag, "br"}},
				nil,
			},
		},
		{
			name:  "sql keywords",
			lang:  sqlLang,
			lines: []string{"SELECT a FROM t;"},
			want: [][]tok{{
				{Keyword, "SELECT"}, {Keyword, "FROM"}, {Punct, ";"},
			}},
		},
		{
			name:  "powershell cmdlet",
			lang:  powershellLang,
			lines: []string{"Get-ChildItem -Path ."},
			want: [][]tok{{
				{Type, "Get-ChildItem"}, {Operator, "-"}, {Type, "Path"}, {Punct, "."},
			}},
		},
		{
			name:  "go",
			lang:  goLang,
			lines: []string{"func main() {"},
			want: [][]tok{{
				{Keyword, "func"},
				{Function, "main"},
				{Punct, "("},
				{Punct, ")"},
				{Punct, "{"},
			}},
		},
		{
			name:  "python",
			lang:  pythonLang,
			lines: []string{"def main():"},
			want: [][]tok{{
				{Keyword, "def"},
				{Function, "main"},
				{Punct, "("},
				{Punct, ")"},
				{Operator, ":"},
			}},
		},
		{
			name:  "rust",
			lang:  rustLang,
			lines: []string{"fn main() {"},
			want: [][]tok{{
				{Keyword, "fn"},
				{Function, "main"},
				{Punct, "("},
				{Punct, ")"},
				{Punct, "{"},
			}},
		},
		{
			name:  "typescript",
			lang:  typescriptLang,
			lines: []string{"export const answer = 42;"},
			want: [][]tok{{
				{Keyword, "export"},
				{Keyword, "const"},
				{Operator, "="},
				{Number, "42"},
				{Punct, ";"},
			}},
		},
		{
			name:  "java declaration",
			lang:  javaLang,
			lines: []string{"public static void main(String[] args) {"},
			want: [][]tok{{
				{Keyword, "public"},
				{Keyword, "static"},
				{Keyword, "void"},
				{Function, "main"},
				{Punct, "("},
				{Type, "String"},
				{Punct, "["},
				{Punct, "]"},
				{Punct, ")"},
				{Punct, "{"},
			}},
		},
		{
			name:  "ruby",
			lang:  rubyLang,
			lines: []string{"def greet(name)"},
			want: [][]tok{{
				{Keyword, "def"}, {Function, "greet"}, {Punct, "("}, {Punct, ")"},
			}},
		},
		{
			name:  "kotlin",
			lang:  kotlinLang,
			lines: []string{"fun main() {"},
			want: [][]tok{{
				{Keyword, "fun"},
				{Function, "main"},
				{Punct, "("},
				{Punct, ")"},
				{Punct, "{"},
			}},
		},
		{
			name:  "swift",
			lang:  swiftLang,
			lines: []string{"func greet() {"},
			want: [][]tok{{
				{Keyword, "func"},
				{Function, "greet"},
				{Punct, "("},
				{Punct, ")"},
				{Punct, "{"},
			}},
		},
		{
			name:  "haskell",
			lang:  haskellLang,
			lines: []string{"main :: IO ()"},
			want: [][]tok{{
				{Operator, "::"}, {Type, "IO"}, {Punct, "("}, {Punct, ")"},
			}},
		},
		{
			name:  "ocaml",
			lang:  ocamlLang,
			lines: []string{"let rec fact n ="},
			want: [][]tok{{
				{Keyword, "let"}, {Keyword, "rec"}, {Operator, "="},
			}},
		},
		{
			name:  "clojure",
			lang:  clojureLang,
			lines: []string{"(defn greet [name]"},
			want: [][]tok{{
				{Punct, "("}, {Keyword, "defn"}, {Punct, "["}, {Punct, "]"},
			}},
		},
		{
			name:  "erlang",
			lang:  erlangLang,
			lines: []string{"    case reverse(List) of"},
			want: [][]tok{{
				{Keyword, "case"},
				{Function, "reverse"},
				{Punct, "("},
				{Type, "List"},
				{Punct, ")"},
				{Keyword, "of"},
			}},
		},
		{
			// A colon is the remote-call separator here, so the callee is a
			// function, not an atom the way ruby reads `:name`.
			name:  "erlang remote call",
			lang:  erlangLang,
			lines: []string{"    lists:reverse(List)."},
			want: [][]tok{{
				{Operator, ":"},
				{Function, "reverse"},
				{Punct, "("},
				{Type, "List"},
				{Punct, ")"},
				{Punct, "."},
			}},
		},
		{
			name:  "elixir",
			lang:  elixirLang,
			lines: []string{"defmodule Demo do"},
			want: [][]tok{{
				{Keyword, "defmodule"}, {Type, "Demo"}, {Keyword, "do"},
			}},
		},
		{
			name:  "scala",
			lang:  scalaLang,
			lines: []string{"object Main extends App {"},
			want: [][]tok{{
				{Keyword, "object"},
				{Type, "Main"},
				{Keyword, "extends"},
				{Type, "App"},
				{Punct, "{"},
			}},
		},
		{
			name:  "dart",
			lang:  dartLang,
			lines: []string{"void main() {"},
			want: [][]tok{{
				{Keyword, "void"},
				{Function, "main"},
				{Punct, "("},
				{Punct, ")"},
				{Punct, "{"},
			}},
		},
		{
			name:  "r",
			lang:  rLang,
			lines: []string{"mean <- function(x) {"},
			want: [][]tok{{
				{Operator, "<-"},
				{Keyword, "function"},
				{Punct, "("},
				{Punct, ")"},
				{Punct, "{"},
			}},
		},
		{
			name:  "perl",
			lang:  perlLang,
			lines: []string{"my $x = 1;"},
			want: [][]tok{{
				{Keyword, "my"},
				{Constant, "$x"},
				{Operator, "="},
				{Number, "1"},
				{Punct, ";"},
			}},
		},
		{
			name:  "verilog",
			lang:  verilogLang,
			lines: []string{"module top(input wire clk);"},
			want: [][]tok{{
				{Keyword, "module"},
				{Function, "top"},
				{Punct, "("},
				{Keyword, "input"},
				{Keyword, "wire"},
				{Punct, ")"},
				{Punct, ";"},
			}},
		},
		{
			// Fortran folds case, so the table's lowercase word matches the
			// uppercase spelling a program uses.
			name:  "fortran",
			lang:  fortranLang,
			lines: []string{"PROGRAM hello"},
			want:  [][]tok{{{Keyword, "PROGRAM"}}},
		},
		{
			name:  "tcl",
			lang:  tclLang,
			lines: []string{"proc greet {name} {"},
			want: [][]tok{{
				{Keyword, "proc"}, {Punct, "{"}, {Punct, "}"}, {Punct, "{"},
			}},
		},
		{
			name:  "vim",
			lang:  vimLang,
			lines: []string{"function! Greet()"},
			want: [][]tok{{
				{Keyword, "function"},
				{Operator, "!"},
				{Function, "Greet"},
				{Punct, "("},
				{Punct, ")"},
			}},
		},
		{
			name:  "lisp",
			lang:  lispLang,
			lines: []string{"(define (square x)"},
			want: [][]tok{{
				{Punct, "("}, {Keyword, "define"}, {Punct, "("}, {Punct, ")"},
			}},
		},
		{
			name:  "raku",
			lang:  rakuLang,
			lines: []string{"sub greet($name) {"},
			want: [][]tok{{
				{Keyword, "sub"},
				{Function, "greet"},
				{Punct, "("},
				{Constant, "$name"},
				{Punct, ")"},
				{Punct, "{"},
			}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x := indexLines(t, c.lang, c.lines)
			for i, line := range c.lines {
				got := lineTokens(x, line, i+1)
				if !equalTokens(got, c.want[i]) {
					t.Errorf("line %d %q: tokens = %v, want %v", i+1, line, got, c.want[i])
				}
			}
		})
	}
}
