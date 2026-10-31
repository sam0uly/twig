package syntax

import "testing"

// heredocLines tokenises lines end to end as one side of a diff, which is how
// a heredoc works: the opener line is scanned, the body lines are decided by
// the terminator table, and the state carries the heredoc flag across lines.
func heredocLines(t *testing.T, lang *Language, lines []string) [][]tok {
	t.Helper()
	b := NewBuilder(lang)
	for i, l := range lines {
		b.Context(l, i+1, i+1)
	}
	idx := b.Index()
	out := make([][]tok, len(lines))
	for i, l := range lines {
		out[i] = spansOf(idx.Old(i+1), l)
	}
	return out
}

func TestHeredocs(t *testing.T) {
	cases := []struct {
		name  string
		lang  *Language
		lines []string
		want  [][]tok
	}{
		{
			name:  "bash heredoc",
			lang:  shellLang,
			lines: []string{"cat <<EOF", "hello $WORLD", "EOF", "echo done"},
			want: [][]tok{
				{{Operator, "<<"}, {String, "EOF"}},
				{{String, "hello $WORLD"}},
				{{String, "EOF"}},
				{{Keyword, "done"}},
			},
		},
		{
			name:  "bash dash heredoc with tab-indented terminator",
			lang:  shellLang,
			lines: []string{"cat <<-EOF", "\tSELECT 1", "\tEOF"},
			want: [][]tok{
				{{Operator, "<<-"}, {String, "EOF"}},
				{{String, "\tSELECT 1"}},
				{{String, "\tEOF"}},
			},
		},
		{
			name:  "bash quoted heredoc",
			lang:  shellLang,
			lines: []string{"cat <<'EOF'", "$HOME unchanged", "EOF"},
			want: [][]tok{
				{{Operator, "<<"}, {String, "'EOF'"}},
				{{String, "$HOME unchanged"}},
				{{String, "EOF"}},
			},
		},
		{
			name:  "bash double-quoted heredoc",
			lang:  shellLang,
			lines: []string{`cat <<"EOF"`, "body", "EOF"},
			want: [][]tok{
				{{Operator, "<<"}, {String, `"EOF"`}},
				{{String, "body"}},
				{{String, "EOF"}},
			},
		},
		{
			name:  "bash escaped heredoc",
			lang:  shellLang,
			lines: []string{`cat <<\EOF`, "body", "EOF"},
			want: [][]tok{
				{{Operator, `<<\`}, {String, "EOF"}},
				{{String, "body"}},
				{{String, "EOF"}},
			},
		},
		{
			name:  "bash two heredocs on one line",
			lang:  shellLang,
			lines: []string{"cat <<A <<B", "body A", "A", "body B", "B"},
			want: [][]tok{
				{{Operator, "<<"}, {String, "A"}, {Operator, "<<"}, {String, "B"}},
				{{String, "body A"}},
				{{String, "A"}},
				{{String, "body B"}},
				{{String, "B"}},
			},
		},
		{
			name:  "ruby squiggly heredoc",
			lang:  rubyLang,
			lines: []string{"sql = <<~SQL", "  SELECT 1", "SQL"},
			want: [][]tok{
				{{Operator, "="}, {Operator, "<<~"}, {String, "SQL"}},
				{{String, "  SELECT 1"}},
				{{String, "SQL"}},
			},
		},
		{
			name:  "ruby indented terminator",
			lang:  rubyLang,
			lines: []string{"a = <<-TXT", "  hi", "  TXT"},
			want: [][]tok{
				{{Operator, "="}, {Operator, "<<-"}, {String, "TXT"}},
				{{String, "  hi"}},
				{{String, "  TXT"}},
			},
		},
		{
			name:  "perl heredoc",
			lang:  perlLang,
			lines: []string{"print <<EOF;", "text $x", "EOF"},
			want: [][]tok{
				{{Operator, "<<"}, {String, "EOF"}, {Punct, ";"}},
				{{String, "text $x"}},
				{{String, "EOF"}},
			},
		},
		{
			name:  "php heredoc closes on a non-word byte",
			lang:  phpLang,
			lines: []string{"$x = <<<EOT", "SELECT 1", "EOT;"},
			want: [][]tok{
				{{Constant, "$x"}, {Operator, "="}, {Operator, "<<<"}, {String, "EOT"}},
				{{String, "SELECT 1"}},
				{{String, "EOT;"}},
			},
		},
		{
			name:  "php quoted heredoc",
			lang:  phpLang,
			lines: []string{"$x = <<<'EOT'", "yes", "EOT;"},
			want: [][]tok{
				{{Constant, "$x"}, {Operator, "="}, {Operator, "<<<"}, {String, "'EOT'"}},
				{{String, "yes"}},
				{{String, "EOT;"}},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := heredocLines(t, c.lang, c.lines)
			for i, want := range c.want {
				if !equalTokens(got[i], want) {
					t.Fatalf("line %d: tokens = %v, want %v", i+1, got[i], want)
				}
			}
		})
	}
}

// TestHeredocNotOpener checks the word-character guard: a "<<" glued to a name
// or a variable is the append/shift operator, not a heredoc.
func TestHeredocNotOpener(t *testing.T) {
	cases := []struct {
		name string
		lang *Language
		src  string
		want []tok
	}{
		{"shift between names", shellLang, `a << b`, []tok{{Operator, "<<"}}},
		{"append to a name", shellLang, `cat<<EOF`, []tok{{Operator, "<<"}}},
		{"shift a variable", shellLang, `2 << $n`, []tok{{Number, "2"}, {Operator, "<<"}, {Constant, "$n"}}},
		{"ruby append", rubyLang, `out<<EOF`, []tok{{Operator, "<<"}, {Constant, "EOF"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, st := c.lang.Line(nil, c.src, 0)
			if st.inHeredoc() {
				t.Fatal("a guarded opener opened a heredoc")
			}
			if words := tokens(c.src, got); !equalTokens(words, c.want) {
				t.Fatalf("tokens = %v, want %v", words, c.want)
			}
		})
	}
}

// TestHeredocUnterminated requires an unterminated heredoc to stay a heredoc
// until the end of the file rather than leaking into code.
func TestHeredocUnterminated(t *testing.T) {
	b := NewBuilder(shellLang)
	b.Context("cat <<EOF", 1, 1)
	b.Context("never closed", 2, 2)
	idx := b.Index()
	if got := spansOf(idx.Old(2), "never closed"); len(got) != 1 || got[0] != (tok{String, "never closed"}) {
		t.Fatalf("line 2 = %v", got)
	}
	if !b.old.inHeredoc() {
		t.Fatal("heredoc should still be open at EOF")
	}
	if b.old != b.new {
		t.Fatal("the two sides should share the open heredoc")
	}
}

// TestHeredocOverflow opens nine heredocs on one line against eight slots; the
// ninth must fall back to today's operator-plus-word colouring, and the eight
// must still close in the order they opened.
func TestHeredocOverflow(t *testing.T) {
	b := NewBuilder(shellLang)
	const line = "cat <<A <<B <<C <<D <<E <<F <<G <<H <<I"
	b.Context(line, 1, 1)
	idx := b.Index()

	want := make([]tok, 0, 17)
	for _, w := range []string{"A", "B", "C", "D", "E", "F", "G", "H"} {
		want = append(want, tok{Operator, "<<"}, tok{String, w})
	}
	want = append(want, tok{Operator, "<<"}) // the ninth "<<I" stayed plain
	if got := spansOf(idx.Old(1), line); !equalTokens(got, want) {
		t.Fatalf("tokens = %v, want %v", got, want)
	}
	if b.oldHD.n != heredocSlots {
		t.Fatalf("%d open slots, want %d", b.oldHD.n, heredocSlots)
	}

	for i, w := range []string{"A", "B", "C", "D", "E", "F", "G", "H"} {
		b.Context(w, i+2, i+2)
	}
	if b.old.inHeredoc() {
		t.Fatal("state still open after every terminator")
	}
}
