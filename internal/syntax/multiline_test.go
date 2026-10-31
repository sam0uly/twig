package syntax

import "testing"

// TestPreprocessor covers the C family's '#' directives: the '#' and the
// directive name are keywords and #include/#import carries a string operand.
func TestPreprocessor(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
		src  string
		want []tok
	}{
		{"c include", cLang, "#include <stdio.h>", []tok{
			{Keyword, "#"}, {Keyword, "include"}, {String, "<stdio.h>"},
		}},
		{"c quoted include", cLang, `#include "a.h"`, []tok{
			{Keyword, "#"}, {Keyword, "include"}, {String, `"a.h"`},
		}},
		{"c conditional", cLang, "#ifdef DEBUG", []tok{
			{Keyword, "#"}, {Keyword, "ifdef"},
		}},
		{"cpp pragma", cppLang, "#pragma once", []tok{
			{Keyword, "#"}, {Keyword, "pragma"},
		}},
		{"objc import", objcLang, "#import <Foundation/Foundation.h>", []tok{
			{Keyword, "#"}, {Keyword, "import"}, {String, "<Foundation/Foundation.h>"},
		}},
		// '#' is a comment in all of these, so the flag stays off.
		{"python", pythonLang, "#include <x>", []tok{{Comment, "#include <x>"}}},
		{"shell", shellLang, "#define X 1", []tok{{Comment, "#define X 1"}}},
		{"yaml", yamlLang, "#include <x>", []tok{{Comment, "#include <x>"}}},
		{"ruby", rubyLang, "#include <x>", []tok{{Comment, "#include <x>"}}},
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

// TestPreprocessorContinuation carries a trailing backslash in State, and the
// continuation line is still the directive's: a leading '#' there is the
// stringify operator, not a new directive.
func TestPreprocessorContinuation(t *testing.T) {
	got, st := cLang.Line(nil, "#define M(x) \\", 0)
	if st&statePreproc == 0 {
		t.Fatalf("opener state = %d, want the preprocessor bit", st)
	}
	if words := tokens("#define M(x) \\", got); !equalTokens(words, []tok{
		{Keyword, "#"}, {Keyword, "define"}, {Punct, "("}, {Punct, ")"},
	}) {
		t.Fatalf("opener tokens = %v", words)
	}
	const body = "  #x \\"
	got, st = cLang.Line(nil, body, st)
	if st&statePreproc == 0 {
		t.Fatalf("body state = %d, want the preprocessor bit", st)
	}
	if len(got) != 0 {
		t.Fatalf("body tokens = %v, want none: '#' is not a directive here", tokens(body, got))
	}
	got, st = cLang.Line(nil, "  2", st)
	if st&statePreproc != 0 {
		t.Fatalf("closer state = %d, want the bit cleared", st)
	}
	if words := tokens("  2", got); !equalTokens(words, []tok{{Number, "2"}}) {
		t.Fatalf("closer tokens = %v", words)
	}
}

// TestBlockCommentBeatsLineComment opens a block comment whose opener starts
// with the same byte as the line comment: lua "--[[", julia "#=" and nim "#[".
func TestBlockCommentBeatsLineComment(t *testing.T) {
	cases := []struct {
		name   string
		lang   *Language
		opener string
		closer string
	}{
		{"lua", luaLang, "--[[ open", " close ]]"},
		{"julia", juliaLang, "#= open", " close =#"},
		{"nim", nimLang, "#[ open", " close ]#"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, st := c.lang.Line(nil, c.opener, 0)
			if st&stateBlock == 0 {
				t.Fatalf("opener state = %d, want an open block", st)
			}
			if words := tokens(c.opener, got); !equalTokens(words, []tok{{Comment, c.opener}}) {
				t.Fatalf("opener tokens = %v", words)
			}
			got, st = c.lang.Line(nil, "body", st)
			if st&stateBlock == 0 {
				t.Fatalf("body state = %d, want an open block", st)
			}
			if words := tokens("body", got); !equalTokens(words, []tok{{Comment, "body"}}) {
				t.Fatalf("body tokens = %v", words)
			}
			got, st = c.lang.Line(nil, c.closer, st)
			if st != 0 {
				t.Fatalf("closer state = %d, want 0", st)
			}
			if words := tokens(c.closer, got); !equalTokens(words, []tok{{Comment, c.closer}}) {
				t.Fatalf("closer tokens = %v", words)
			}
		})
	}
}

// TestNestedBlockCommentOpener pins the nesting of julia and nim blocks, and
// that a bare comment marker still opens a line comment.
func TestNestedBlockCommentOpener(t *testing.T) {
	for _, c := range []struct {
		name string
		lang *Language
		src  string
	}{
		{"julia", juliaLang, "#= a #= b =# c =#"},
		{"nim", nimLang, "#[ a #[ b ]# c ]#"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, st := c.lang.Line(nil, c.src, 0)
			if st != 0 {
				t.Fatalf("state = %d, want 0", st)
			}
			if words := tokens(c.src, got); !equalTokens(words, []tok{{Comment, c.src}}) {
				t.Fatalf("tokens = %v", words)
			}
		})
	}
	got, _ := juliaLang.Line(nil, "# plain", 0)
	if words := tokens("# plain", got); !equalTokens(words, []tok{{Comment, "# plain"}}) {
		t.Fatalf("julia line comment = %v", words)
	}
}

// TestZigMultiLineString pins the line-prefix string: a line whose first
// non-blank bytes are "\\" is one String, and it carries no state.
func TestZigMultiLineString(t *testing.T) {
	const src = `    \\hello world`
	got, st := zigLang.Line(nil, src, 0)
	if words := tokens(src, got); !equalTokens(words, []tok{{String, `\\hello world`}}) {
		t.Fatalf("tokens = %v", words)
	}
	if st != 0 {
		t.Fatalf("state = %d, want 0", st)
	}
	const code = `const x = 1;`
	got, _ = zigLang.Line(nil, code, 0)
	if words := tokens(code, got); !equalTokens(words, []tok{
		{Keyword, "const"}, {Operator, "="}, {Number, "1"}, {Punct, ";"},
	}) {
		t.Fatalf("code tokens = %v", words)
	}
}

// TestNixIndentedString opens a two-quote indented string, resumes it on the
// next line with its escapes in the body, and closes it.
func TestNixIndentedString(t *testing.T) {
	got, st := nixLang.Line(nil, "s = ''", 0)
	if words := tokens("s = ''", got); !equalTokens(words, []tok{
		{Operator, "="}, {String, "''"},
	}) {
		t.Fatalf("opener tokens = %v", words)
	}
	if st != stringState(1, false) {
		t.Fatalf("opener state = %d, want the second string rule", st)
	}
	const body = "  hi ${x}"
	got, st = nixLang.Line(nil, body, st)
	if words := tokens(body, got); !equalTokens(words, []tok{{String, body}}) {
		t.Fatalf("body tokens = %v", words)
	}
	if !st.inString() {
		t.Fatal("body closed the string")
	}
	// ''${ is an escaped interpolation and ''' an escaped quote, so neither
	// is a closer.
	const escaped = "a ''${y} ''' b"
	got, st = nixLang.Line(nil, escaped, st)
	if words := tokens(escaped, got); !equalTokens(words, []tok{{String, escaped}}) {
		t.Fatalf("escape tokens = %v", words)
	}
	got, st = nixLang.Line(nil, "''", st)
	if words := tokens("''", got); !equalTokens(words, []tok{{String, "''"}}) {
		t.Fatalf("closer tokens = %v", words)
	}
	if st != 0 {
		t.Fatalf("closer state = %d, want 0", st)
	}
	// A lone quote is not an opener.
	got, _ = nixLang.Line(nil, "x = '", 0)
	if words := tokens("x = '", got); !equalTokens(words, []tok{{Operator, "="}}) {
		t.Fatalf("lone quote tokens = %v", words)
	}
}

// TestHCLHeredoc runs HCL's <<EOT and <<-EOT through the existing heredoc
// queue: the body is String and the terminator closes it, indented for <<-.
func TestHCLHeredoc(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  [][]tok
	}{
		{
			name:  "hcl indented heredoc",
			lines: []string{"x = <<-EOT", "  body ${y}", "  EOT", "y = 1"},
			want: [][]tok{
				{{Key, "x"}, {Operator, "="}, {Operator, "<<-"}, {String, "EOT"}},
				{{String, "  body ${y}"}},
				{{String, "  EOT"}},
				{{Key, "y"}, {Operator, "="}, {Number, "1"}},
			},
		},
		{
			name:  "hcl plain heredoc",
			lines: []string{"x = <<EOT", "body", "EOT"},
			want: [][]tok{
				{{Key, "x"}, {Operator, "="}, {Operator, "<<"}, {String, "EOT"}},
				{{String, "body"}},
				{{String, "EOT"}},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := heredocLines(t, hclLang, c.lines)
			for i, want := range c.want {
				if !equalTokens(got[i], want) {
					t.Fatalf("line %d: tokens = %v, want %v", i+1, got[i], want)
				}
			}
		})
	}
	// HCL's word is bare, so a quoted one is not an opener.
	if _, st := hclLang.Line(nil, "x = <<'EOT'", 0); st.inHeredoc() {
		t.Fatal("a quoted HCL word opened a heredoc")
	}
}
