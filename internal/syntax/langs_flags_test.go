package syntax

import "testing"

// TestPHPNoKeySep pins the removal of PHP's '=' KeySep: "foo = 1" is an
// assignment, not an object key, while "$foo" stays a variable.
func TestPHPNoKeySep(t *testing.T) {
	got, _ := phpLang.Line(nil, "foo = 1", 0)
	if words := tokens("foo = 1", got); !equalTokens(words, []tok{
		{Operator, "="},
		{Number, "1"},
	}) {
		t.Fatalf("tokens = %v", words)
	}
	got, _ = phpLang.Line(nil, "$foo = 1", 0)
	if words := tokens("$foo = 1", got); !equalTokens(words, []tok{
		{Constant, "$foo"},
		{Operator, "="},
		{Number, "1"},
	}) {
		t.Fatalf("variable tokens = %v", words)
	}
}

// TestOCamlNoLineComment pins the removal of "//": two slashes are two
// operator tokens, while the (* *) block comment still closes.
func TestOCamlNoLineComment(t *testing.T) {
	got, _ := ocamlLang.Line(nil, "// note here", 0)
	if words := tokens("// note here", got); !equalTokens(words, []tok{
		{Operator, "//"},
	}) {
		t.Fatalf("slash tokens = %v", words)
	}
	got, _ = ocamlLang.Line(nil, "(* a comment *)", 0)
	if words := tokens("(* a comment *)", got); !equalTokens(words, []tok{
		{Comment, "(* a comment *)"},
	}) {
		t.Fatalf("block tokens = %v", words)
	}
}

// TestTripleQuoted opens and closes a """ block across lines. Java, Kotlin and
// Swift get the rule here; the rest already had it and must keep it.
func TestTripleQuoted(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
	}{
		{"java", javaLang},
		{"kotlin", kotlinLang},
		{"swift", swiftLang},
		{"python", pythonLang},
		{"scala", scalaLang},
		{"julia", juliaLang},
		{"elixir", elixirLang},
		{"groovy", groovyLang},
		{"dart", dartLang},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, st := c.lang.Line(nil, `s = """`, 0)
			if words := tokens(`s = """`, got); !equalTokens(words, []tok{
				{Operator, "="},
				{String, `"""`},
			}) {
				t.Fatalf("opener tokens = %v", words)
			}
			if st != stringState(0, true) {
				t.Fatalf("opener state = %d, want triple", st)
			}
			got, st = c.lang.Line(nil, `hello " world`, st)
			if words := tokens(`hello " world`, got); !equalTokens(words, []tok{
				{String, `hello " world`},
			}) {
				t.Fatalf("middle tokens = %v", words)
			}
			if st != stringState(0, true) {
				t.Fatalf("middle state = %d, want triple", st)
			}
			got, st = c.lang.Line(nil, `"""`, st)
			if words := tokens(`"""`, got); !equalTokens(words, []tok{
				{String, `"""`},
			}) {
				t.Fatalf("closer tokens = %v", words)
			}
			if st != 0 {
				t.Fatalf("closer state = %d, want 0", st)
			}
		})
	}
}

// TestGroovyTripleSingle pins Repeat on Groovy's single-quote rule.
func TestGroovyTripleSingle(t *testing.T) {
	got, _ := groovyLang.Line(nil, "def s = '''abc'''", 0)
	if words := tokens("def s = '''abc'''", got); !equalTokens(words, []tok{
		{Keyword, "def"},
		{Operator, "="},
		{String, "'''abc'''"},
	}) {
		t.Fatalf("tokens = %v", words)
	}
}

// TestDartFlags covers Dart's nesting block comments and its doc comments.
func TestDartFlags(t *testing.T) {
	got, _ := dartLang.Line(nil, "/* outer /* inner */ still */", 0)
	if words := tokens("/* outer /* inner */ still */", got); !equalTokens(words, []tok{
		{Comment, "/* outer /* inner */ still */"},
	}) {
		t.Fatalf("nested tokens = %v", words)
	}
	got, _ = dartLang.Line(nil, "/// Adds one.", 0)
	if words := tokens("/// Adds one.", got); !equalTokens(words, []tok{
		{Doc, "/// Adds one."},
	}) {
		t.Fatalf("line doc tokens = %v", words)
	}
	got, _ = dartLang.Line(nil, "/** Adds one. */", 0)
	if words := tokens("/** Adds one. */", got); !equalTokens(words, []tok{
		{Doc, "/** Adds one. */"},
	}) {
		t.Fatalf("block doc tokens = %v", words)
	}
}

// TestRegexLanguages pins the plain /re/ form for Ruby and Perl: a regex in
// expression position is a String, and division stays an Operator.
func TestRegexLanguages(t *testing.T) {
	got, _ := rubyLang.Line(nil, "x = /ab+c/i", 0)
	if words := tokens("x = /ab+c/i", got); !equalTokens(words, []tok{
		{Operator, "="},
		{String, "/ab+c/i"},
	}) {
		t.Fatalf("ruby regex tokens = %v", words)
	}
	got, _ = rubyLang.Line(nil, "a / b", 0)
	if words := tokens("a / b", got); !equalTokens(words, []tok{
		{Operator, "/"},
	}) {
		t.Fatalf("ruby division tokens = %v", words)
	}
	got, _ = perlLang.Line(nil, "$x = /ab+c/i", 0)
	if words := tokens("$x = /ab+c/i", got); !equalTokens(words, []tok{
		{Constant, "$x"},
		{Operator, "="},
		{String, "/ab+c/i"},
	}) {
		t.Fatalf("perl regex tokens = %v", words)
	}
}

// TestDocBlockFlags covers the block doc form for the tables that spell it
// /** */ or {-| -}.
func TestDocBlockFlags(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
		src  string
		want []tok
	}{
		{"haskell", haskellLang, "{-| A doc comment. -}", []tok{{Doc, "{-| A doc comment. -}"}}},
		{"scala", scalaLang, "/** Scaladoc. */", []tok{{Doc, "/** Scaladoc. */"}}},
		{"dart", dartLang, "/** A doc block. */", []tok{{Doc, "/** A doc block. */"}}},
		{"csharp", csharpLang, "/// A summary.", []tok{{Doc, "/// A summary."}}},
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

// TestHyphenKeys pins Hyphen on the equals-separated config tables: "my-key"
// is one Key token, not "key" with a stray minus.
func TestHyphenKeys(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
	}{
		{"toml", tomlLang},
		{"ini", iniLang},
		{"hcl", hclLang},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := c.lang.Line(nil, "my-key = 1", 0)
			if words := tokens("my-key = 1", got); !equalTokens(words, []tok{
				{Key, "my-key"},
				{Operator, "="},
				{Number, "1"},
			}) {
				t.Fatalf("tokens = %v", words)
			}
		})
	}
}

// TestNixNestedComment pins Nest on nixLang: Nix block comments balance, so the
// comment closes at the */ that matches the outer opener, while a plain single
// comment still closes at the first */.
func TestNixNestedComment(t *testing.T) {
	got, _ := nixLang.Line(nil, "/* a /* b */ c */ d", 0)
	if words := tokens("/* a /* b */ c */ d", got); !equalTokens(words, []tok{
		{Comment, "/* a /* b */ c */"},
	}) {
		t.Fatalf("nested tokens = %v", words)
	}
	got, _ = nixLang.Line(nil, "/* a */ d", 0)
	if words := tokens("/* a */ d", got); !equalTokens(words, []tok{
		{Comment, "/* a */"},
	}) {
		t.Fatalf("single tokens = %v", words)
	}
}

// TestPHPAttribute keeps php 8's "#[...]" attribute from reading as a '#' line
// comment and painting the whole line.
func TestPHPAttribute(t *testing.T) {
	const src = `#[Route('/x')]`
	got, _ := phpLang.Line(nil, src, 0)
	if words := tokens(src, got); !equalTokens(words, []tok{
		{Punct, "["},
		{Function, "Route"},
		{Punct, "("},
		{String, "'/x'"},
		{Punct, ")"},
		{Punct, "]"},
	}) {
		t.Fatalf("attribute tokens = %v", words)
	}
	got, _ = phpLang.Line(nil, "# a comment", 0)
	if words := tokens("# a comment", got); !equalTokens(words, []tok{{Comment, "# a comment"}}) {
		t.Fatalf("comment tokens = %v", words)
	}
}

// TestVimCommentAndString pins the split between vim's " comment and its "
// string quote: a quote starts a comment only as the first non-blank byte.
func TestVimCommentAndString(t *testing.T) {
	cases := []struct {
		src  string
		want []tok
	}{
		{`" a comment`, []tok{{Comment, `" a comment`}}},
		{`echo "hi"`, []tok{{Keyword, "echo"}, {String, `"hi"`}}},
		{`let s = 'single'`, []tok{{Keyword, "let"}, {Operator, "="}, {String, "'single'"}}},
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			got, _ := vimLang.Line(nil, c.src, 0)
			if words := tokens(c.src, got); !equalTokens(words, c.want) {
				t.Fatalf("tokens = %v, want %v", words, c.want)
			}
		})
	}
}

// TestSQLKeywordsNoCase pins NoCase on the lowercase SQL table, and that the
// doubled single quote stays string-coloured on both sides.
func TestSQLKeywordsNoCase(t *testing.T) {
	got, _ := sqlLang.Line(nil, "SELECT id FROM users", 0)
	if words := tokens("SELECT id FROM users", got); !equalTokens(words, []tok{
		{Keyword, "SELECT"},
		{Keyword, "FROM"},
	}) {
		t.Fatalf("tokens = %v", words)
	}
	got, _ = sqlLang.Line(nil, "select id from users", 0)
	if words := tokens("select id from users", got); !equalTokens(words, []tok{
		{Keyword, "select"},
		{Keyword, "from"},
	}) {
		t.Fatalf("lowercase tokens = %v", words)
	}
	got, _ = sqlLang.Line(nil, "SELECT 'it''s' FROM t", 0)
	if words := tokens("SELECT 'it''s' FROM t", got); !equalTokens(words, []tok{
		{Keyword, "SELECT"},
		{String, "'it'"},
		{String, "'s'"},
		{Keyword, "FROM"},
	}) {
		t.Fatalf("quoted tokens = %v", words)
	}
}

// TestPowerShellCmdletHyphen keeps a cmdlet's verb-noun one identifier while a
// parameter's leading hyphen stays an operator.
func TestPowerShellCmdletHyphen(t *testing.T) {
	got, _ := powershellLang.Line(nil, "Get-ChildItem", 0)
	if words := tokens("Get-ChildItem", got); !equalTokens(words, []tok{{Type, "Get-ChildItem"}}) {
		t.Fatalf("cmdlet tokens = %v", words)
	}
	got, _ = powershellLang.Line(nil, "Get-ChildItem -Path", 0)
	if words := tokens("Get-ChildItem -Path", got); !equalTokens(words, []tok{
		{Type, "Get-ChildItem"},
		{Operator, "-"},
		{Type, "Path"},
	}) {
		t.Fatalf("parameter tokens = %v", words)
	}
	got, _ = powershellLang.Line(nil, "FOREACH", 0)
	if words := tokens("FOREACH", got); !equalTokens(words, []tok{{Keyword, "FOREACH"}}) {
		t.Fatalf("shouted keyword tokens = %v", words)
	}
}
