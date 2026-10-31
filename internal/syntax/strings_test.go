package syntax

import "testing"

// TestRawStrings covers the raw string families whose terminator is not the
// ordinary quote: Rust, Swift, C#, C++ and Lua.
func TestRawStrings(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
		src  string
		st   State
		want []tok
		end  State
	}{
		{
			name: "rust raw string",
			lang: rustLang,
			src:  `let s = r"a\nb";`,
			want: []tok{
				{Keyword, "let"},
				{Operator, "="},
				{String, `r"a\nb"`},
				{Punct, ";"},
			},
		},
		{
			name: "rust hashed raw string",
			lang: rustLang,
			src:  `let s = r#"a"#;`,
			want: []tok{
				{Keyword, "let"},
				{Operator, "="},
				{String, `r#"a"#`},
				{Punct, ";"},
			},
		},
		{
			name: "rust byte raw string",
			lang: rustLang,
			src:  `let s = br#"a"#;`,
			want: []tok{
				{Keyword, "let"},
				{Operator, "="},
				{String, `br#"a"#`},
				{Punct, ";"},
			},
		},
		{
			name: "rust r identifier is not a raw string",
			lang: rustLang,
			src:  `let r = 1;`,
			want: []tok{
				{Keyword, "let"},
				{Operator, "="},
				{Number, "1"},
				{Punct, ";"},
			},
		},
		{
			name: "rust raw string opens across lines",
			lang: rustLang,
			src:  `let s = r#"a`,
			want: []tok{
				{Keyword, "let"},
				{Operator, "="},
				{String, `r#"a`},
			},
			end: rawStringState(RawRust, 1, 0),
		},
		{
			name: "rust raw string resumes and closes",
			lang: rustLang,
			src:  `b"# + 1;`,
			st:   rawStringState(RawRust, 1, 0),
			want: []tok{
				{String, `b"#`},
				{Operator, "+"},
				{Number, "1"},
				{Punct, ";"},
			},
		},
		{
			name: "swift raw string",
			lang: swiftLang,
			src:  `let s = #"a\nb"#`,
			want: []tok{
				{Keyword, "let"},
				{Operator, "="},
				{String, `#"a\nb"#`},
			},
		},
		{
			name: "swift double hash raw string",
			lang: swiftLang,
			src:  `let s = ##"a"##`,
			want: []tok{
				{Keyword, "let"},
				{Operator, "="},
				{String, `##"a"##`},
			},
		},
		{
			name: "swift sharp is not forced into a string",
			lang: swiftLang,
			src:  `#if DEBUG`,
			want: []tok{{Keyword, "if"}, {Constant, "DEBUG"}},
		},
		{
			name: "csharp verbatim string",
			lang: csharpLang,
			src:  `var p = @"a""b";`,
			want: []tok{
				{Keyword, "var"},
				{Operator, "="},
				{String, `@"a""b"`},
				{Punct, ";"},
			},
		},
		{
			name: "csharp raw string",
			lang: csharpLang,
			src:  `var s = """abc""";`,
			want: []tok{
				{Keyword, "var"},
				{Operator, "="},
				{String, `"""abc"""`},
				{Punct, ";"},
			},
		},
		{
			name: "csharp raw string opens across lines",
			lang: csharpLang,
			src:  `var s = """abc`,
			want: []tok{
				{Keyword, "var"},
				{Operator, "="},
				{String, `"""abc`},
			},
			end: rawStringState(RawCSharpRaw, 3, 0),
		},
		{
			name: "csharp raw string resumes and closes",
			lang: csharpLang,
			src:  `def""";`,
			st:   rawStringState(RawCSharpRaw, 3, 0),
			want: []tok{
				{String, `def"""`},
				{Punct, ";"},
			},
		},
		{
			name: "csharp ordinary string untouched",
			lang: csharpLang,
			src:  `var s = "a";`,
			want: []tok{
				{Keyword, "var"},
				{Operator, "="},
				{String, `"a"`},
				{Punct, ";"},
			},
		},
		{
			name: "cpp raw string",
			lang: cppLang,
			src:  `auto s = R"(a (b) c)";`,
			want: []tok{
				{Keyword, "auto"},
				{Operator, "="},
				{String, `R"(a (b) c)"`},
				{Punct, ";"},
			},
		},
		{
			name: "cpp raw string with delimiter",
			lang: cppLang,
			src:  `auto s = R"tag(a)b)tag";`,
			want: []tok{
				{Keyword, "auto"},
				{Operator, "="},
				{String, `R"tag(a)b)tag"`},
				{Punct, ";"},
			},
		},
		{
			name: "lua long string",
			lang: luaLang,
			src:  `local s = [[abc]]`,
			want: []tok{
				{Keyword, "local"},
				{Operator, "="},
				{String, `[[abc]]`},
			},
		},
		{
			name: "lua long string with equals",
			lang: luaLang,
			src:  `local s = [==[a]=b]==]`,
			want: []tok{
				{Keyword, "local"},
				{Operator, "="},
				{String, `[==[a]=b]==]`},
			},
		},
		{
			name: "lua long string opens across lines",
			lang: luaLang,
			src:  `local s = [==[abc`,
			want: []tok{
				{Keyword, "local"},
				{Operator, "="},
				{String, `[==[abc`},
			},
			end: rawStringState(RawLua, 2, 0),
		},
		{
			name: "lua long string resumes and closes",
			lang: luaLang,
			src:  `def]==]`,
			st:   rawStringState(RawLua, 2, 0),
			want: []tok{{String, `def]==]`}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, end := c.lang.Line(nil, c.src, c.st)
			if words := tokens(c.src, got); !equalTokens(words, c.want) {
				t.Fatalf("tokens = %v, want %v", words, c.want)
			}
			if end != c.end {
				t.Fatalf("state = %d, want %d", end, c.end)
			}
		})
	}
}

// TestCppRawDelimiterAcrossLines checks that a C++ raw string with a custom
// delimiter resumes on the next line through the builder's side table.
func TestCppRawDelimiterAcrossLines(t *testing.T) {
	b := NewBuilder(cppLang)
	b.Context("auto s = R\"ABC(a", 1, 1)
	b.Context(`b)ABC";`, 2, 2)
	idx := b.Index()

	if got := spansOf(idx.Old(1), "auto s = R\"ABC(a"); !equalTokens(got, []tok{
		{Keyword, "auto"},
		{Operator, "="},
		{String, `R"ABC(a`},
	}) {
		t.Fatalf("opener line = %v", got)
	}
	if got := spansOf(idx.Old(2), `b)ABC";`); !equalTokens(got, []tok{
		{String, `b)ABC"`},
		{Punct, ";"},
	}) {
		t.Fatalf("closer line = %v", got)
	}
}

// TestNestedBlockComments covers the languages whose block comments nest, and
// keeps a non-nesting language on the old "first close" behaviour.
func TestNestedBlockComments(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
		src  string
		st   State
		want []tok
		end  State
	}{
		{
			name: "rust comment nests",
			lang: rustLang,
			src:  "let x = /* a /* b */ c */ 1;",
			want: []tok{
				{Keyword, "let"},
				{Operator, "="},
				{Comment, "/* a /* b */ c */"},
				{Number, "1"},
				{Punct, ";"},
			},
		},
		{
			name: "swift comment nests",
			lang: swiftLang,
			src:  "/* outer /* inner */ still */",
			want: []tok{{Comment, "/* outer /* inner */ still */"}},
		},
		{
			name: "kotlin comment nests",
			lang: kotlinLang,
			src:  "/* outer /* inner */ still */",
			want: []tok{{Comment, "/* outer /* inner */ still */"}},
		},
		{
			name: "scala comment nests",
			lang: scalaLang,
			src:  "/* outer /* inner */ still */",
			want: []tok{{Comment, "/* outer /* inner */ still */"}},
		},
		{
			name: "haskell comment nests",
			lang: haskellLang,
			src:  "{- outer {- inner -} still -}",
			want: []tok{{Comment, "{- outer {- inner -} still -}"}},
		},
		{
			name: "elm comment nests",
			lang: elmLang,
			src:  "{- outer {- inner -} still -}",
			want: []tok{{Comment, "{- outer {- inner -} still -}"}},
		},
		{
			name: "ocaml comment nests",
			lang: ocamlLang,
			src:  "(* outer (* inner *) still *)",
			want: []tok{{Comment, "(* outer (* inner *) still *)"}},
		},
		{
			name: "go comment does not nest",
			lang: goLang,
			src:  "/* a /* b */ c */",
			want: []tok{
				{Comment, "/* a /* b */"},
				{Operator, "*/"},
			},
		},
		{
			name: "rust nested comment opens across lines",
			lang: rustLang,
			src:  "/* a /* b",
			want: []tok{{Comment, "/* a /* b"}},
			end:  blockState(false, 2),
		},
		{
			name: "rust nested comment resumes and balances",
			lang: rustLang,
			src:  " c */ d */ x",
			st:   blockState(false, 2),
			want: []tok{{Comment, " c */ d */"}},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, end := c.lang.Line(nil, c.src, c.st)
			if words := tokens(c.src, got); !equalTokens(words, c.want) {
				t.Fatalf("tokens = %v, want %v", words, c.want)
			}
			if end != c.end {
				t.Fatalf("state = %d, want %d", end, c.end)
			}
		})
	}
}

// TestRegexLiterals covers "/" in expression position for JavaScript and
// TypeScript: a regex with its flags, the class and escape grammar, and the
// fallbacks that keep division and compound assignment operators.
func TestRegexLiterals(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
		src  string
		want []tok
	}{
		{
			name: "javascript regex with flags",
			lang: javascriptLang,
			src:  "const re = /foo/g;",
			want: []tok{
				{Keyword, "const"},
				{Operator, "="},
				{String, "/foo/g"},
				{Punct, ";"},
			},
		},
		{
			name: "javascript regex with class and escapes",
			lang: javascriptLang,
			src:  `const re = /[a\/]+\w+/g;`,
			want: []tok{
				{Keyword, "const"},
				{Operator, "="},
				{String, `/[a\/]+\w+/g`},
				{Punct, ";"},
			},
		},
		{
			name: "division stays an operator",
			lang: javascriptLang,
			src:  "a / b / c;",
			want: []tok{
				{Operator, "/"},
				{Operator, "/"},
				{Punct, ";"},
			},
		},
		{
			name: "compound assignment stays an operator",
			lang: javascriptLang,
			src:  "x /= 2;",
			want: []tok{
				{Operator, "/="},
				{Number, "2"},
				{Punct, ";"},
			},
		},
		{
			name: "postfix then divide falls back",
			lang: javascriptLang,
			src:  "a++ / 2;",
			want: []tok{
				{Operator, "++"},
				{Operator, "/"},
				{Number, "2"},
				{Punct, ";"},
			},
		},
		{
			name: "regex without a closer on the line falls back",
			lang: javascriptLang,
			src:  "const re = /foo",
			want: []tok{
				{Keyword, "const"},
				{Operator, "="},
				{Operator, "/"},
			},
		},
		{
			name: "typescript regex",
			lang: typescriptLang,
			src:  "const re: RegExp = /a+/;",
			want: []tok{
				{Keyword, "const"},
				{Key, "re"},
				{Operator, ":"},
				{Type, "RegExp"},
				{Operator, "="},
				{String, "/a+/"},
				{Punct, ";"},
			},
		},
		{
			name: "tsx regex",
			lang: typescriptJSX,
			src:  "const re = /x/g;",
			want: []tok{
				{Keyword, "const"},
				{Operator, "="},
				{String, "/x/g"},
				{Punct, ";"},
			},
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

// TestRegexExprBit carries the "an expression may start here" bit across a line
// break, and clears it after a regex ends the line.
func TestRegexExprBit(t *testing.T) {
	lang := javascriptLang

	// "=" at end of line: the next line may open with a regex literal.
	got, st := lang.Line(nil, "const re =", 0)
	if words := tokens("const re =", got); !equalTokens(words, []tok{
		{Keyword, "const"},
		{Operator, "="},
	}) {
		t.Fatalf("opener tokens = %v", words)
	}
	if st&stateExpr == 0 {
		t.Fatal("stateExpr not set after a trailing =")
	}
	got, _ = lang.Line(nil, "/a+/g;", st)
	if words := tokens("/a+/g;", got); !equalTokens(words, []tok{
		{String, "/a+/g"},
		{Punct, ";"},
	}) {
		t.Fatalf("regex line tokens = %v", words)
	}

	// A regex flag ends the line, so the next line must not start a regex.
	got, st = lang.Line(nil, "const re = /a/g", 0)
	if words := tokens("const re = /a/g", got); len(words) == 0 {
		t.Fatal("no tokens on regex line")
	}
	if st&stateExpr != 0 {
		t.Fatal("stateExpr set after a regex")
	}
	got, _ = lang.Line(nil, "/b/;", st)
	if words := tokens("/b/;", got); !equalTokens(words, []tok{
		{Operator, "/"},
		{Operator, "/"},
		{Punct, ";"},
	}) {
		t.Fatalf("after-regex line tokens = %v", words)
	}
}

// TestCSharpRawQuoteCap pins the opener cap: C# treats a run of three quotes
// as the opener however long it is, so an empty """""" is one closed empty raw
// string, not a six-quote opener whose closer never comes.
func TestCSharpRawQuoteCap(t *testing.T) {
	got, st := csharpLang.Line(nil, `""""""`, 0)
	if words := tokens(`""""""`, got); !equalTokens(words, []tok{{String, `""""""`}}) {
		t.Fatalf("tokens = %v", words)
	}
	if st != 0 {
		t.Fatalf("state = %d, want closed", st)
	}
}

// TestCSharpTextBlock checks that C#'s """ opens a multi-line raw string and
// closes on a line of three quotes.
func TestCSharpTextBlock(t *testing.T) {
	got, st := csharpLang.Line(nil, `string s = """`, 0)
	if words := tokens(`string s = """`, got); !equalTokens(words, []tok{
		{Type, "string"},
		{Operator, "="},
		{String, `"""`},
	}) {
		t.Fatalf("opener tokens = %v", words)
	}
	if st != rawStringState(RawCSharpRaw, 3, 0) {
		t.Fatalf("opener state = %d, want raw", st)
	}
	got, st = csharpLang.Line(nil, `text`, st)
	if words := tokens(`text`, got); !equalTokens(words, []tok{{String, `text`}}) {
		t.Fatalf("body tokens = %v", words)
	}
	got, st = csharpLang.Line(nil, `"""`, st)
	if words := tokens(`"""`, got); !equalTokens(words, []tok{{String, `"""`}}) {
		t.Fatalf("closer tokens = %v", words)
	}
	if st != 0 {
		t.Fatalf("closer state = %d, want closed", st)
	}
}

// TestCSharpVerbatimContinues keeps an unterminated @"..." open across the line
// boundary, which is how C# spells a multi-line verbatim string.
func TestCSharpVerbatimContinues(t *testing.T) {
	got, st := csharpLang.Line(nil, `var s = @"line1`, 0)
	if words := tokens(`var s = @"line1`, got); !equalTokens(words, []tok{
		{Keyword, "var"},
		{Operator, "="},
		{String, `@"line1`},
	}) {
		t.Fatalf("opener tokens = %v", words)
	}
	if st != rawStringState(RawCSharpVerbatim, 0, 0) {
		t.Fatalf("opener state = %d, want raw verbatim", st)
	}
	got, st = csharpLang.Line(nil, `line2";`, st)
	if words := tokens(`line2";`, got); !equalTokens(words, []tok{
		{String, `line2"`},
		{Punct, ";"},
	}) {
		t.Fatalf("continuation tokens = %v", words)
	}
	if st != 0 {
		t.Fatalf("continuation state = %d, want closed", st)
	}
}
