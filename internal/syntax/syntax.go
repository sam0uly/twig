// Package syntax colours source lines by token class.
//
// It is a scanner, not a parser. A diff only needs to tell a keyword from a
// string, a comment from a number and a key from a value, and a scanner
// carrying a word of state across a line boundary does that without the file
// in front of it. The model is the incremental lexer of Reps, Teitelbaum and
// Demers (TOPLAS 1983): a token boundary is a resumable point, so the state
// saved at the end of one line is all the next line needs.
//
// Nothing here allocates on the hot path, nothing is loaded from disk, and no
// grammar is compiled into the binary: the tables in langs.go are the data.
package syntax

import (
	"strings"
	"sync"
	"unicode/utf8"
)

// Class is the kind of a token. The set follows roles rather than grammar: a
// reader separates structure from names, literals from decoration, and the
// classes are what the theme can then colour by role.
type Class uint8

// Token classes. Plain is the absence of a class: text the diff already
// colours and the scanner has nothing to add to.
const (
	Plain Class = iota
	// Keyword is control flow, a declaration, a directive or a tag name.
	Keyword
	// Type is a type, class or interface name.
	Type
	// Function is a call or a definition that is being called.
	Function
	// String is a quoted literal.
	String
	// Escape is an escape sequence inside a string.
	Escape
	// Number is a numeric literal.
	Number
	// Constant is a literal name: nil, a shell variable, or a name shouted by
	// convention.
	Constant
	// Boolean is true and false, which a reader scans for more than any other
	// literal.
	Boolean
	// Key is a name on the left of a separator: an object key, a mapping key,
	// a property, an attribute.
	Key
	// Tag is a markup element name.
	Tag
	// Comment is an ordinary comment.
	Comment
	// Doc is a comment that documents the thing below it.
	Doc
	// Operator is an operator token.
	Operator
	// Punct is a bracket or separator.
	Punct
	// ClassCount is the number of classes, so a style table can be sized by it.
	ClassCount
)

// Span is a byte range of one source line and the class to paint it with.
// Offsets are into the source line, without the diff sign prefix.
type Span struct {
	Start, End int32
	Class      Class
}

// StringRule describes one string delimiter of a language.
type StringRule struct {
	// Quote is the delimiter byte.
	Quote byte
	// Escape is the escape byte, or zero for a raw string.
	Escape byte
	// MultiLine lets the string run past the end of the line.
	MultiLine bool
	// Repeat makes three Quote bytes in a row open a multi-line string, which
	// is how python spells a docstring.
	Repeat bool
	// Pair makes two Quote bytes in a row the delimiter, which is how nix
	// spells its indented string. The same pair also introduces the escapes,
	// so scanString reads ''' and ''${ as body.
	Pair bool
}

// State is the scanner state at a line boundary. It carries everything the next
// line needs: what is open, which string rule opened it, and the JSX mode with
// its nesting. It is wider than a byte because JSX nests, and it is never
// stored per row, so the width costs no memory.
type State uint32

const (
	stateRule   State = 0x0f      // bits 0-3: string rule plus one, or raw kind
	stateTriple State = 1 << 4    // three quotes opened the string
	stateDoc    State = 1 << 5    // the open comment documents its subject
	stateTag    State = 1 << 6    // a markup tag is open
	stateBlock  State = 1 << 7    // a block comment is open
	stateExpr   State = 1 << 8    // an expression may start here
	stateMode   State = 0x3 << 9  // bits 9-10: JSX mode
	stateDepth  State = 0x3 << 11 // bits 11-12: JSX element depth
	stateBrace  State = 0x3 << 13 // bits 13-14: brace depth inside an expression
	stateReturn State = 1 << 15   // a closing tag, or an expression inside one

	// stateRaw marks the open string as a raw string: rule() then names one of
	// the RawKind values below instead of an index into the string table, and
	// run() and delim() carry its terminator.
	stateRaw    State = 1 << 16    // a raw string is open
	stateRun    State = 0xf << 17  // bits 17-20: hash/equal/quote run, saturating
	stateCDepth State = 0x7 << 21  // bits 21-23: block comment nesting, saturating
	stateDelim  State = 0x1f << 24 // bits 24-28: C++ raw delimiter index plus one
	// stateHeredoc marks a here-document open. The State does not name the
	// slot: the per-side table is a FIFO queue, so its front is the heredoc
	// the next line is inside.
	stateHeredoc State = 1 << 29
	// statePreproc marks a preprocessor directive left open by a trailing
	// backslash. The continuation line still belongs to the directive, so a
	// leading '#' on it is the stringify operator and not a new directive.
	statePreproc State = 1 << 30
)

// Literals is the set of literal spellings a language has beyond the C family.
// It is a bit set rather than a field per form because each of these is one
// table's spelling quirk and not a structure the scanner changes shape for.
type Literals uint8

// Literal forms. Each is recognised only where the table turns it on, so no
// other language pays for the branch or reads the bytes differently.
const (
	// LitChar reads ' + one rune or one escape + ' as a character literal,
	// which is how Haskell spells one. Every other quote stays an operator,
	// so foldl' and a Template Haskell 'Name are untouched.
	LitChar Literals = 1 << iota
	// LitBase reads a hash-spelled base literal, which is how Erlang writes
	// 16#FF and 2#1010.
	LitBase
	// LitDollar reads Erlang's $a and $\n character literal.
	LitDollar
	// LitSized reads Verilog's sized literal, 8'hFF and 4'b1010, and the
	// unbased 'bz form.
	LitSized
)

// RawKind identifies the terminator rule of a raw string.
type RawKind uint8

// RawKind names one raw string family. The run() value in the state means the
// number of '#' (Rust, Swift), '=' (Lua), or quotes (C# raw) of its terminator.
const (
	RawRust RawKind = iota + 1
	RawSwift
	RawCSharpVerbatim
	RawCSharpRaw
	RawLua
	RawCpp
)

// JSX modes carried in State.
const (
	modeJS = iota
	modeTag
	modeChildren
	modeExpr
)

// mode is the JSX mode of the state.
func (s State) mode() int { return int(s&stateMode) >> 9 }

// withMode replaces the JSX mode.
func (s State) withMode(m int) State {
	return (s &^ stateMode) | State(m)<<9
}

// depth is how many JSX elements are open.
func (s State) depth() int { return int(s&stateDepth) >> 11 }

// withDepth replaces the element depth, saturating at 3.
func (s State) withDepth(d int) State {
	return (s &^ stateDepth) | State(min(d, 3))<<11
}

// closing reports whether the open tag is a closing tag, which pops instead of
// descending.
func (s State) closing() bool { return s&stateReturn != 0 }

// exprInTag reports whether an open expression will return to a tag rather than
// to children.
func (s State) exprInTag() bool { return s&stateReturn != 0 }

// brace is the brace depth inside a JSX expression.
func (s State) brace() int { return int(s&stateBrace) >> 13 }

// withBrace replaces the brace depth, saturating at 3.
func (s State) withBrace(b int) State {
	return (s &^ stateBrace) | State(min(b, 3))<<13
}

// inBlock reports whether a block comment is open.
func (s State) inBlock() bool { return s&stateBlock != 0 }

// triple reports whether the open string was opened with three quotes.
func (s State) triple() bool { return s&stateTriple != 0 }

// doc reports whether the open comment documents its subject.
func (s State) doc() bool { return s&stateDoc != 0 }

// inTag reports whether a markup tag is open.
func (s State) inTag() bool { return s&stateTag != 0 }

// rule is the index plus one of the open string rule, or zero for none. When
// stateRaw is set it is instead a RawKind value.
func (s State) rule() int { return int(s & stateRule) }

// raw reports whether the open string is a raw string rather than a table rule.
func (s State) raw() bool { return s&stateRaw != 0 }

// run is the raw string's hashes, equals or quotes, whichever its kind uses.
func (s State) run() int { return int(s&stateRun) >> 17 }

// delim is the plus-one index of the open C++ raw delimiter in the builder's
// side table, or zero.
func (s State) delim() int { return int(s&stateDelim) >> 24 }

// cDepth is how many block comments are open, saturating at 7.
func (s State) cDepth() int { return int(s&stateCDepth) >> 21 }

// inHeredoc reports whether a here-document is open.
func (s State) inHeredoc() bool { return s&stateHeredoc != 0 }

// inString reports whether a string is open.
func (s State) inString() bool { return s.rule() != 0 }

// stringState builds the state of an open string.
func stringState(rule int, triple bool) State {
	st := State(rule+1) & stateRule
	if triple {
		st |= stateTriple
	}
	return st
}

// rawStringState builds the state of an open raw string. count is the hash,
// equals or quote run and delimIdx is a plus-one C++ delimiter index.
func rawStringState(kind RawKind, count, delimIdx int) State {
	return stateRaw |
		State(kind) |
		State(min(count, 15))<<17 |
		State(min(delimIdx, 31))<<24
}

// blockState builds the state of an open block comment at a nesting depth of at
// least one.
func blockState(doc bool, depth int) State {
	st := stateBlock | State(min(max(depth, 1), 7))<<21
	if doc {
		st |= stateDoc
	}
	return st
}

// Embed is one region of another language inside this one. The opener is
// matched anywhere on a line and the closer ends the run when it appears at the
// start of a later line; the host wins, so the inner scanner never sees the
// line that closes it.
type Embed struct {
	// Open and Close are literal markers, e.g. "<script" and "</script".
	Open, Close string
	// Lang is the inner language when no attribute says otherwise.
	Lang *Language
	// Attr is an attribute whose value chooses the language, e.g. "lang".
	Attr string
	// ByAttr maps an attribute value to a language.
	ByAttr map[string]*Language
	// Fence marks a markdown fenced code block: a line whose first non-space
	// run of three or more backticks or tildes opens it, and a line with no
	// fewer of the same char, and nothing else, closes it. Open and Close do
	// not apply to a fence.
	Fence bool
}

// Language is one language's spelling: its comment delimiters, its string
// rules, its reserved words, and the few structural hints that need no grammar.
type Language struct {
	Name    string
	Exts    []string
	Files   []string
	LineCmt []string
	// DocCmt are the line comment markers that document rather than annotate.
	DocCmt []string
	Block  [2]string
	// DocBlock treats a block comment opened with a doubled star or bang as a
	// doc comment, which is how javadoc, jsdoc, phpdoc and go's block docs
	// read.
	DocBlock bool
	Strings  []StringRule
	// Keywords, Types, Consts and Bools are space-separated words.
	Keywords string
	Types    string
	Consts   string
	Bools    string
	// KeySep names the separator that turns the name in front of it into a
	// key: ':' for a mapping or an object, '=' for a toml key. Zero turns the
	// rule off.
	KeySep byte
	// Markup turns on tag and attribute scanning.
	Markup bool
	// Heading is the byte that opens a heading when it is the first thing on a
	// line, which is how markdown spells "# Title".
	Heading byte
	// Atoms reads ":name" as a constant, which is how elixir, erlang, ruby and
	// clojure spell a symbol.
	Atoms bool
	// JSX turns on the JSX mode machine: "<" in expression position opens a tag,
	// and the scanner follows tags, children and {expressions} across lines.
	JSX bool
	// Embed lists the regions of another language a file of this one contains:
	// HTML's <script> and <style>, a Vue or Svelte component's blocks.
	Embed []Embed
	// Vars colours "$name" as a constant, which is what a shell or php
	// variable reads like at a glance.
	Vars bool
	// Hyphen lets an identifier carry a hyphen, which yaml keys, css
	// properties, html attributes and powershell cmdlets all spell that way.
	Hyphen bool
	// Attrs keeps "#[" from opening a line comment, which is how php 8 spells
	// an attribute.
	Attrs bool
	// CommentFirst makes the line comment markers count only as the first
	// non-blank byte of a line, which is how vim script tells its " comment
	// from its " string quote.
	CommentFirst bool
	// Hints turns on the two guesses that need no grammar: an identifier
	// followed by "(" is a call, and an identifier starting uppercase is a
	// type.
	Hints bool
	// Raw lists the language's raw string forms. Each opens with a terminator
	// that is not the ordinary quote: Rust r"..." / r#"..."#, Swift #"..."#,
	// C# @"..." and """...""", Lua [[...]], C++ R"delim(...)delim".
	Raw []RawKind
	// Nest makes block comments nest, which is how Rust, Swift, Haskell, Elm,
	// OCaml, Scala and Kotlin close them at the outer delimiter rather than
	// the first one.
	Nest bool
	// NoCase matches reserved words in any case, which is how Fortran and
	// Basic spell them. The table indexes the lower and upper spellings of
	// each word at prepare time, so a line still looks words up exactly and
	// the no-allocation promise holds.
	NoCase bool
	// Regex makes "/" open a regex literal where an expression may start,
	// which is how JavaScript and TypeScript spell one.
	Regex bool
	// Heredoc is how the language spells a here-document, or hdNone. The
	// scanner's only cost for the languages without one is the flag test.
	Heredoc heredocKind
	// Preproc turns on preprocessor directives, where a line whose first
	// non-blank byte is '#' names a directive rather than opening a comment.
	// Only the C family gets it: for python, shell or yaml a '#' is a comment.
	Preproc bool
	// LineStr opens a string at the start of a line that begins with it and
	// runs to the end of that line, which is how zig spells its multi-line
	// string. Empty turns the rule off.
	LineStr string
	// Mustache scans {{ ... }} regions in markup text and attribute values as
	// JavaScript, which is how a template spells an interpolation.
	Mustache bool
	// Vue scans the values of :attr, @attr, #attr and v- directives as
	// JavaScript, which is how a Vue or Svelte component spells an expression.
	Vue bool
	// Literals adds the literal spellings beyond the C family that only this
	// table has: Haskell's 'x', Erlang's 16#FF and $a, Verilog's 8'hFF.
	Literals Literals
	// Interp colours the "$" of an interpolated string as part of the
	// literal, which is how C# spells $"..." and $@"...".
	Interp bool

	once sync.Once
	p    prepared
}

// prepared is the lookup shape of a Language. It is built on first use rather
// than at package initialisation, so starting twig costs nothing.
type prepared struct {
	words      map[string]Class
	lc         []string
	lcFirst    [256]bool
	doc        []string
	docFirst   [256]bool
	blockOpen  string
	blockClose string
	quote      [256]int8
	hasBlock   bool
	docBlock   bool
	strings    []StringRule
	keySep     byte
	markup     bool
	vars       bool
	hyphen     bool
	attrs      bool
	cmtFirst   bool
	heading    byte
	atoms      bool
	raw        []RawKind
	rawFirst   [256]bool
	nest       bool
	regex      bool
	hd         heredocKind
	preproc    bool
	lineStr    string
	mustache   bool
	vue        bool
	lits       Literals
	interp     bool
}

// prepared returns the language's lookup tables, building them once.
func (l *Language) prepared() *prepared {
	l.once.Do(func() {
		p := &l.p
		if l.NoCase {
			p.words = wordSetFold(l.Keywords, l.Types, l.Consts, l.Bools)
		} else {
			p.words = wordSet(l.Keywords, l.Types, l.Consts, l.Bools)
		}
		p.lc = l.LineCmt
		for _, m := range l.LineCmt {
			if m != "" {
				p.lcFirst[m[0]] = true
			}
		}
		p.doc = l.DocCmt
		for _, m := range l.DocCmt {
			if m != "" {
				p.docFirst[m[0]] = true
			}
		}
		p.blockOpen, p.blockClose = l.Block[0], l.Block[1]
		p.hasBlock = p.blockOpen != "" && p.blockClose != ""
		p.docBlock = l.DocBlock
		for i, r := range l.Strings {
			if i >= int(stateRule)-1 {
				break
			}
			if p.quote[r.Quote] == 0 {
				p.quote[r.Quote] = int8(i + 1)
			}
		}
		p.strings = l.Strings
		p.keySep = l.KeySep
		p.markup = l.Markup
		p.vars = l.Vars
		p.hyphen = l.Hyphen
		p.attrs = l.Attrs
		p.cmtFirst = l.CommentFirst
		p.heading = l.Heading
		p.atoms = l.Atoms
		p.raw = l.Raw
		for _, k := range l.Raw {
			switch k {
			case RawRust:
				p.rawFirst['r'] = true
				p.rawFirst['b'] = true
			case RawSwift:
				p.rawFirst['#'] = true
			case RawCSharpVerbatim:
				p.rawFirst['@'] = true
			case RawCSharpRaw:
				p.rawFirst['"'] = true
			case RawLua:
				p.rawFirst['['] = true
			case RawCpp:
				p.rawFirst['R'] = true
			}
		}
		p.nest = l.Nest
		p.regex = l.Regex
		p.hd = l.Heredoc
		p.preproc = l.Preproc
		p.lineStr = l.LineStr
		p.mustache = l.Mustache
		p.vue = l.Vue
		p.lits = l.Literals
		p.interp = l.Interp
		if l.Interp {
			// An interpolated string opens on the raw rule's gate, so one
			// byte test per character covers both.
			p.rawFirst['$'] = true
		}
	})
	return &l.p
}

// wordSet indexes three space-separated word lists by class. One map of class
// beats three sets: a line is mostly identifiers, so a lookup saved per
// identifier is most of the scanner's cost.
func wordSet(lists ...string) map[string]Class {
	classes := []Class{Keyword, Type, Constant, Boolean}
	n := 0
	for _, list := range lists {
		n += len(strings.Fields(list))
	}
	if n == 0 {
		return nil
	}
	set := make(map[string]Class, n)
	for i, list := range lists {
		for _, w := range strings.Fields(list) {
			set[w] = classes[i]
		}
	}
	return set
}

// wordSetFold is wordSet for a case-insensitive language: each word is indexed
// under its lower and its upper spelling, so classify keeps its exact,
// non-allocating lookup and mixed-case identifiers simply fall through.
func wordSetFold(lists ...string) map[string]Class {
	classes := []Class{Keyword, Type, Constant, Boolean}
	n := 0
	for _, list := range lists {
		n += len(strings.Fields(list)) * 2
	}
	if n == 0 {
		return nil
	}
	set := make(map[string]Class, n)
	for i, list := range lists {
		for _, w := range strings.Fields(list) {
			lo, hi := strings.ToLower(w), strings.ToUpper(w)
			set[lo], set[hi] = classes[i], classes[i]
		}
	}
	return set
}

// Line tokenises one line, appending its spans to dst and returning the state
// at the end of the line. It never allocates: dst is the caller's buffer.
func (l *Language) Line(dst []Span, src string, st State) ([]Span, State) {
	var noDels []string
	return l.line(dst, src, st, &noDels)
}

// line is Line with the builder's C++ raw delimiter side table, so a raw string
// that spans lines can resume on the next one. Direct Line callers get a fresh
// empty table and degrade to a generic terminator if they cross a boundary.
func (l *Language) line(dst []Span, src string, st State, dels *[]string) ([]Span, State) {
	return l.scanLine(dst, src, st, dels, nil)
}

// scanLine is line plus a heredoc table: the scanner reports each opener into
// hds, and the state it returns is stateHeredoc once one opened. The Builder
// is the caller that has a table; Line uses nil.
func (l *Language) scanLine(dst []Span, src string, st State, dels *[]string, hds *heredocSet) ([]Span, State) {
	if l.JSX && st.mode() != modeJS {
		return l.lineJSX(dst, src, st, dels)
	}
	before := uint8(0)
	if hds != nil {
		before = hds.n
	}
	if l.Markup {
		return l.lineMarkup(dst, src, st)
	}
	dst, st = l.lineJS(dst, src, 0, st, dels, hds)
	if hds != nil && hds.n > before {
		st = stateHeredoc
	}
	return dst, st
}

// lineJS scans a line as JavaScript, TypeScript or any other table, starting at
// byte start. A nonzero start is a resume after a JSX tag opened mid-line, so
// the open-construct switch only runs when the line begins here.
func (l *Language) lineJS(dst []Span, src string, start int, st State, dels *[]string, hds *heredocSet) ([]Span, State) {
	p := l.prepared()
	n := len(src)
	i := start // the loop resumes here; nothing resets it before the switch

	// The hot loop appends to dst, which the compiler must assume can alias the
	// language's tables, so every field read through p would be a fresh load.
	// Copying what the loop needs into locals keeps them in registers.
	words := p.words
	quote := &p.quote
	docFirst := &p.docFirst
	lcFirst := &p.lcFirst
	rawFirst := &p.rawFirst
	docMarkers, lcMarkers := p.doc, p.lc
	blockOpen, blockClose, hasBlock := p.blockOpen, p.blockClose, p.hasBlock
	docBlock := p.docBlock
	rules := p.strings
	keySep, markup, vars, hyphen := p.keySep, p.markup, p.vars, p.hyphen
	heading, atoms := p.heading, p.atoms
	attrs, cmtFirst := p.attrs, p.cmtFirst
	hd := p.hd
	preproc, lineStr := p.preproc, p.lineStr
	jsx := l.JSX
	hasRaw := len(p.raw) > 0 || p.interp
	nest := p.nest
	regex := p.regex
	lits, interp := p.lits, p.interp
	inExpr := st.mode() == modeExpr
	brace := st.brace()
	// directive records that this line carried a preprocessor directive, so
	// a trailing backslash hands the next line to it too.
	directive := preproc && st&statePreproc != 0

	// first is the index of the first non-blank byte, which is where a heading
	// marker has to be to count as one.
	first := 0
	for first < n && isSpaceByte(src[first]) {
		first++
	}

	// Resume whatever the previous line left open.
	switch {
	case st.inBlock():
		class := Comment
		if st.doc() {
			class = Doc
		}
		if nest {
			// A nesting language counts open and close delimiters: the comment
			// ends at the one that balances the opens, not at the first close.
			end, d := scanBlockNested(src, 0, blockOpen, blockClose, st.cDepth())
			if n > 0 || end > 0 {
				dst = append(dst, Span{0, int32(end), class})
			}
			if d != 0 {
				return dst, blockState(st.doc(), d)
			}
			i, st = end, 0
		} else {
			if k := strings.Index(src, blockClose); k >= 0 {
				end := k + len(blockClose)
				dst = append(dst, Span{0, int32(end), class})
				i, st = end, 0
			} else {
				if n > 0 {
					dst = append(dst, Span{0, int32(n), class})
				}
				return dst, st
			}
		}
	case st.inString():
		if st.raw() {
			end, closed := scanRaw(src, 0, RawKind(st.rule()), st.run(), resDelim(dels, st.delim()))
			if end > 0 {
				dst = append(dst, Span{0, int32(end), String})
			}
			if !closed {
				return dst, st
			}
			i, st = end, 0
		} else {
			rule := &p.strings[st.rule()-1]
			end, closed := scanString(src, 0, rule, st.triple())
			dst = appendString(dst, src, 0, end, rule)
			if !closed {
				return dst, st
			}
			i, st = end, 0
		}
	}

	inTag := st.inTag()

	for i < n {
		c := src[i]

		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		if i == first {
			if heading != 0 && c == heading {
				dst = append(dst, Span{int32(i), int32(n), Keyword})
				return dst, 0
			}
			// zig's only multi-line string is a line that opens with
			// "\\\\"; the whole rest of that line is the literal.
			if lineStr != "" && c == lineStr[0] && strings.HasPrefix(src[i:], lineStr) {
				dst = append(dst, Span{int32(i), int32(n), String})
				return dst, 0
			}
			if preproc && !directive && c == '#' {
				dst, i = preprocDirective(dst, src, i)
				directive = true
				continue
			}
		}
		// The block check comes before the line comment check: lua, julia
		// and nim open a block comment with the same byte their line comment
		// starts with, and at the same offset the block opener is the longer
		// and the truer reading.
		if hasBlock && c == blockOpen[0] && strings.HasPrefix(src[i:], blockOpen) {
			open := i + len(blockOpen)
			class, doc := Comment, false
			if docBlock && isDocBlock(src[i:], blockOpen, blockClose) {
				class, doc = Doc, true
			}
			if nest {
				end, d := scanBlockNested(src, open, blockOpen, blockClose, 1)
				dst = append(dst, Span{int32(i), int32(end), class})
				if d != 0 {
					return dst, blockState(doc, d)
				}
				i = end
				continue
			}
			if k := strings.Index(src[open:], blockClose); k >= 0 {
				end := open + k + len(blockClose)
				dst = append(dst, Span{int32(i), int32(end), class})
				i = end
				continue
			}
			dst = append(dst, Span{int32(i), int32(n), class})
			end := stateBlock
			if doc {
				end |= stateDoc
			}
			return dst, end
		}
		if docFirst[c] && markerAt(src, i, docMarkers) {
			dst = append(dst, Span{int32(i), int32(n), Doc})
			return dst, 0
		}
		if lcFirst[c] && (!cmtFirst || i == first) {
			// php 8 spells an attribute "#[", which a '#' comment marker
			// would otherwise swallow.
			attr := attrs && c == '#' && i+1 < n && src[i+1] == '['
			if !attr && lineCommentAt(lcMarkers, src, i) {
				dst = append(dst, Span{int32(i), int32(n), Comment})
				return dst, 0
			}
		}
		if markup {
			if c == '<' {
				inTag = true
				i++
				if i < n && src[i] == '/' {
					i++
				}
				if i < n && isIdentStart(src[i]) {
					end := scanIdent(src, i, hyphen)
					dst = append(dst, Span{int32(i), int32(end), Tag})
					i = end
				}
				continue
			}
			if inTag && c == '>' {
				inTag = false
				i++
				continue
			}
			if inTag && isIdentStart(c) {
				end := scanIdent(src, i, hyphen)
				dst = append(dst, Span{int32(i), int32(end), Key})
				i = end
				continue
			}
		}
		if hasRaw && rawFirst[c] {
			// The $ of a C# interpolated string belongs to the literal, and
			// an @ in its prefix makes the body verbatim, so both of those
			// scan from the quote as one string. It shares the raw opener's
			// gate, which is one byte test for every character either way.
			if interp && (c == '$' || c == '@') {
				if body, verbatim, ok := interpOpen(src, i); ok {
					r := quote['"']
					if verbatim || r != 0 {
						var end int
						var closed bool
						if verbatim {
							end, closed = scanRaw(src, body, RawCSharpVerbatim, 0, "")
							dst = append(dst, Span{int32(i), int32(end), String})
						} else {
							rule := &rules[r-1]
							end, closed = scanString(src, body, rule, false)
							dst = appendString(dst, src, i, end, rule)
						}
						if !closed {
							if verbatim {
								return dst, rawStringState(RawCSharpVerbatim, 0, 0)
							}
							return dst, stringState(int(r)-1, false)
						}
						i = end
						continue
					}
				}
			}
			if kind, count, body, delim, ok := rawOpen(src, i, c); ok {
				end, closed := scanRaw(src, body, kind, count, delim)
				dst = append(dst, Span{int32(i), int32(end), String})
				if !closed {
					di := 0
					if kind == RawCpp {
						di = addDelim(dels, delim)
					}
					return dst, rawStringState(kind, count, di)
				}
				i = end
				continue
			}
		}
		if r := quote[c]; r != 0 {
			rule := &rules[r-1]
			open, triple := 1, false
			if rule.Repeat && i+2 < n && src[i+1] == rule.Quote && src[i+2] == rule.Quote {
				open, triple = 3, true
			}
			if rule.Pair && (i+1 >= n || src[i+1] != rule.Quote) {
				// A lone quote does not open a two-quote rule: nix has no
				// single-quoted string, so the byte is just punctuation.
				open = 0
			} else if rule.Pair {
				open = 2
			}
			if open == 0 {
				i++
				continue
			}
			end, closed := scanString(src, i+open, rule, triple)
			if keySep != 0 && closed && !triple && keyFollows(src, end, keySep) {
				dst = append(dst, Span{int32(i), int32(end), Key})
			} else {
				dst = appendString(dst, src, i, end, rule)
			}
			if !closed {
				return dst, stringState(int(r)-1, triple)
			}
			i = end
			continue
		}
		if atoms && c == ':' && i+1 < n && isIdentStart(src[i+1]) {
			end := scanIdent(src, i+1, hyphen)
			dst = append(dst, Span{int32(i), int32(end), Constant})
			i = end
			continue
		}
		if vars && c == '$' {
			if end := scanVar(src, i); end > 0 {
				dst = append(dst, Span{int32(i), int32(end), Constant})
				i = end
				continue
			}
		}
		if lits != 0 {
			// The quote byte as this language spells it: haskell's 'x' is a
			// string and verilog's 'bz a number, and a quote that closes
			// neither stays an operator. Erlang's $a is the other sigil-as-
			// literal.
			switch {
			case c == '\'' && lits&LitChar != 0:
				if end := scanChar(src, i); end > 0 {
					dst = append(dst, Span{int32(i), int32(end), String})
					i = end
					continue
				}
			case c == '\'' && lits&LitSized != 0:
				if end := scanSized(src, i); end > 0 {
					dst = append(dst, Span{int32(i), int32(end), Number})
					i = end
					continue
				}
			case c == '$' && lits&LitDollar != 0:
				if end := scanDollarLit(src, i); end > 0 {
					dst = append(dst, Span{int32(i), int32(end), Number})
					i = end
					continue
				}
			}
		}
		if isDigit(c) || (c == '.' && i+1 < n && isDigit(src[i+1])) {
			end := scanNumber(src, i, lits)
			dst = append(dst, Span{int32(i), int32(end), Number})
			i = end
			continue
		}
		if isIdentStart(c) {
			end := scanIdent(src, i, hyphen)
			class := l.classify(words, src, i, end)
			if class == Plain || class == Type {
				if keySep != 0 && keyFollows(src, end, keySep) {
					class = Key
				}
			}
			if class != Plain {
				dst = append(dst, Span{int32(i), int32(end), class})
			}
			i = end
			continue
		}
		if jsx && !inExpr && c == '<' && jsxAllowed(src, i, st) {
			var ok bool
			dst, i, st, ok = l.openTag(dst, src, i, st)
			if ok {
				switch st.mode() {
				case modeTag:
					return l.lineTag(dst, src, i, st, dels)
				case modeChildren:
					return l.lineChildren(dst, src, i, st, dels)
				}
				continue
			}
		}
		if inExpr && (c == '{' || c == '}') {
			dst = append(dst, Span{int32(i), int32(i + 1), Punct})
			i++
			if c == '{' {
				brace = min(brace+1, 3)
				st = st.withBrace(brace)
				continue
			}
			if brace > 0 {
				brace--
				st = st.withBrace(brace)
				continue
			}
			back := modeChildren
			if st.exprInTag() {
				back = modeTag
			}
			st = st.withMode(back).withBrace(0) &^ stateReturn
			return l.continueAt(dst, src, i, st, dels)
		}
		if regex && c == '/' && i+1 < n && src[i+1] != '=' && exprOKAt(src, i, st) {
			// "/" where an expression may start is a regex literal. scanRegex
			// demands a closing "/" and its flags on this line; failing that it
			// returns not-ok and "/" stays an operator, which is what protects
			// "a++ / 2" and a stray "</div>".
			if end, ok := scanRegex(src, i+1); ok {
				dst = append(dst, Span{int32(i), int32(end), String})
				i = end
				continue
			}
		}
		if hd != hdNone && c == '<' {
			word, fl, opEnd, strStart, strEnd, ok := parseHeredoc(hd, src, i)
			if ok && hds != nil && len(word) <= heredocWordMax {
				if slot := hds.open(word, fl); slot >= 0 {
					dst = append(dst, Span{int32(i), int32(opEnd), Operator})
					dst = append(dst, Span{int32(strStart), int32(strEnd), String})
					i = strEnd
					continue
				}
			}
			// Not a heredoc, or the table is full, or the terminator is too
			// long: fall through and colour the "<" as today's operator.
		}
		if isOp(c) {
			end := i + 1
			for end < n && isOp(src[end]) {
				end++
			}
			dst = append(dst, Span{int32(i), int32(end), Operator})
			i = end
			continue
		}
		if isPunct(c) {
			dst = append(dst, Span{int32(i), int32(i + 1), Punct})
		}
		i++
	}

	end := State(0)
	if inTag {
		end |= stateTag
	}
	if jsx {
		end |= st & (stateMode | stateDepth | stateBrace | stateReturn)
		if inExpr {
			end = end.withMode(modeExpr).withBrace(brace)
		}
	}
	if regex && exprOKAfter(src) {
		// A regex literal ends an expression rather than starting one, so the
		// trailing byte decides: after "/.../g" the bit lands false, after "="
		// at end of line it stays true for a regex on the next line.
		end |= stateExpr
	}
	if directive && lineContinues(src) {
		// A directive ending in "\\" keeps the next line, which is how a
		// macro body or a long include path spans lines.
		end |= statePreproc
	}
	return dst, end
}

// lineMarkup scans a line of a markup language: HTML, a Vue or Svelte
// component, or a .php page. It is lineJS plus the template expressions that
// only markup tables enable, kept out of the plain-language loop so Go and
// Python pay nothing for them: {{ ... }} mustaches and JavaScript-valued
// attribute expressions.
func (l *Language) lineMarkup(dst []Span, src string, st State) ([]Span, State) {
	p := l.prepared()
	n := len(src)
	i := 0

	// The hot loop appends to dst, which the compiler must assume can alias the
	// language's tables, so every field read through p would be a fresh load.
	// Copying what the loop needs into locals keeps them in registers.
	words := p.words
	quote := &p.quote
	docFirst := &p.docFirst
	lcFirst := &p.lcFirst
	docMarkers, lcMarkers := p.doc, p.lc
	blockOpen, blockClose, hasBlock := p.blockOpen, p.blockClose, p.hasBlock
	docBlock := p.docBlock
	rules := p.strings
	keySep, markup, vars, hyphen := p.keySep, p.markup, p.vars, p.hyphen
	heading, atoms := p.heading, p.atoms
	attrs, cmtFirst := p.attrs, p.cmtFirst
	mustache, vue := p.mustache, p.vue

	// first is the index of the first non-blank byte, which is where a heading
	// marker has to be to count as one.
	first := 0
	for first < n && isSpaceByte(src[first]) {
		first++
	}

	// Resume whatever the previous line left open.
	switch {
	case st.inBlock():
		class := Comment
		if st.doc() {
			class = Doc
		}
		if k := strings.Index(src, blockClose); k >= 0 {
			end := k + len(blockClose)
			dst = append(dst, Span{0, int32(end), class})
			i, st = end, 0
		} else {
			if n > 0 {
				dst = append(dst, Span{0, int32(n), class})
			}
			return dst, st
		}
	case st.inString():
		rule := &p.strings[st.rule()-1]
		end, closed := scanString(src, 0, rule, st.triple())
		dst = appendString(dst, src, 0, end, rule)
		if !closed {
			return dst, st
		}
		i, st = end, 0
	}

	inTag := st.inTag()
	attrVue := false

	for i < n {
		c := src[i]

		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		if heading != 0 && i == first && c == heading {
			dst = append(dst, Span{int32(i), int32(n), Keyword})
			return dst, 0
		}
		// The block check comes before the line comment check for the same
		// reason as in lineJS: the longer opener is the truer reading.
		if hasBlock && c == blockOpen[0] && strings.HasPrefix(src[i:], blockOpen) {
			open := i + len(blockOpen)
			class, doc := Comment, false
			if docBlock && isDocBlock(src[i:], blockOpen, blockClose) {
				class, doc = Doc, true
			}
			if k := strings.Index(src[open:], blockClose); k >= 0 {
				end := open + k + len(blockClose)
				dst = append(dst, Span{int32(i), int32(end), class})
				i = end
				continue
			}
			dst = append(dst, Span{int32(i), int32(n), class})
			end := stateBlock
			if doc {
				end |= stateDoc
			}
			return dst, end
		}
		if docFirst[c] && markerAt(src, i, docMarkers) {
			dst = append(dst, Span{int32(i), int32(n), Doc})
			return dst, 0
		}
		if lcFirst[c] && (!cmtFirst || i == first) {
			// php 8 spells an attribute "#[", which a '#' comment marker
			// would otherwise swallow.
			attr := attrs && c == '#' && i+1 < n && src[i+1] == '['
			if !attr && lineCommentAt(lcMarkers, src, i) {
				dst = append(dst, Span{int32(i), int32(n), Comment})
				return dst, 0
			}
		}
		if markup {
			if c == '<' {
				inTag = true
				attrVue = false
				i++
				if i < n && src[i] == '/' {
					i++
				}
				if i < n && isIdentStart(src[i]) {
					end := scanIdent(src, i, hyphen)
					dst = append(dst, Span{int32(i), int32(end), Tag})
					i = end
				}
				continue
			}
			if inTag && c == '/' && i+1 < n && src[i+1] == '>' {
				// "/>" is one operator run, so a self-closing tag has to
				// step over the slash to reach the '>' that closes it.
				i++
				continue
			}
			if inTag && c == '>' {
				inTag = false
				attrVue = false
				i++
				continue
			}
			if inTag && isIdentStart(c) {
				end := scanIdent(src, i, hyphen)
				dst = append(dst, Span{int32(i), int32(end), Key})
				if vue {
					// A dot keeps a directive's value flag across its
					// modifiers: @click.prevent="go" is still an expression.
					if i == 0 || src[i-1] != '.' {
						attrVue = vueAttrName(src, i, end)
					}
				}
				i = end
				continue
			}
			if mustache && !inTag && c == '{' && i+1 < n && src[i+1] == '{' {
				if close := mustacheClose(src, i+2, n); close >= 0 {
					dst = append(dst, Span{int32(i), int32(i + 2), Punct})
					dst = appendChild(dst, javascriptLang, src, i+2, close)
					dst = append(dst, Span{int32(close), int32(close + 2), Punct})
					i = close + 2
					continue
				}
				// No closer on this line: leave the rest to the host.
			}
		}
		if r := quote[c]; r != 0 {
			rule := &rules[r-1]
			open, triple := 1, false
			if rule.Repeat && i+2 < n && src[i+1] == rule.Quote && src[i+2] == rule.Quote {
				open, triple = 3, true
			}
			if rule.Pair && (i+1 >= n || src[i+1] != rule.Quote) {
				// A lone quote does not open a two-quote rule: nix has no
				// single-quoted string, so the byte is just punctuation.
				open = 0
			} else if rule.Pair {
				open = 2
			}
			if open == 0 {
				i++
				continue
			}
			end, closed := scanString(src, i+open, rule, triple)
			closedHere := closed && !triple && end > i+open && src[end-1] == c
			if vue && inTag && attrVue && closedHere {
				// A Vue attribute value is an expression: <div :class="a ? 'b' : 'c'">.
				dst = append(dst, Span{int32(i), int32(i + 1), Punct})
				dst = appendChild(dst, javascriptLang, src, i+1, end-1)
				dst = append(dst, Span{int32(end - 1), int32(end), Punct})
				attrVue = false
				i = end
				continue
			}
			if mustache && closedHere && strings.Contains(src[i+open:end-1], "{{") {
				// A mustache inside an attribute value: <a title="{{ msg }}">.
				dst = append(dst, Span{int32(i), int32(i + 1), String})
				dst = appendMustaches(dst, src, i+1, end-1)
				dst = append(dst, Span{int32(end - 1), int32(end), String})
				i = end
				continue
			}
			if keySep != 0 && closed && !triple && keyFollows(src, end, keySep) {
				dst = append(dst, Span{int32(i), int32(end), Key})
			} else {
				dst = appendString(dst, src, i, end, rule)
			}
			if !closed {
				return dst, stringState(int(r)-1, triple)
			}
			i = end
			continue
		}
		if atoms && c == ':' && i+1 < n && isIdentStart(src[i+1]) {
			end := scanIdent(src, i+1, hyphen)
			dst = append(dst, Span{int32(i), int32(end), Constant})
			i = end
			continue
		}
		if vars && c == '$' {
			if end := scanVar(src, i); end > 0 {
				dst = append(dst, Span{int32(i), int32(end), Constant})
				i = end
				continue
			}
		}
		if isDigit(c) || (c == '.' && i+1 < n && isDigit(src[i+1])) {
			end := scanNumber(src, i, 0)
			dst = append(dst, Span{int32(i), int32(end), Number})
			i = end
			continue
		}
		if isIdentStart(c) {
			end := scanIdent(src, i, hyphen)
			class := l.classify(words, src, i, end)
			if class == Plain || class == Type {
				if keySep != 0 && keyFollows(src, end, keySep) {
					class = Key
				}
			}
			if class != Plain {
				dst = append(dst, Span{int32(i), int32(end), class})
			}
			i = end
			continue
		}
		if isOp(c) {
			end := i + 1
			for end < n && isOp(src[end]) {
				end++
			}
			dst = append(dst, Span{int32(i), int32(end), Operator})
			i = end
			continue
		}
		if isPunct(c) {
			dst = append(dst, Span{int32(i), int32(i + 1), Punct})
		}
		i++
	}

	end := State(0)
	if inTag {
		end |= stateTag
	}
	return dst, end
}

// appendString adds a string run, splitting its escape sequences out so a "\n"
// reads as an escape and not as more string.
func appendString(dst []Span, src string, from, to int, rule *StringRule) []Span {
	start := from
	for j := from; j < to; {
		if rule.Escape != 0 && src[j] == rule.Escape && j+1 < to {
			n := 2
			if _, w := utf8.DecodeRuneInString(src[j+1 : to]); w > 1 {
				// An escape covers the backslash and the byte it escapes;
				// widen it over the whole rune so a span boundary never
				// splits a character of valid UTF-8.
				n = 1 + w
			}
			if start < j {
				dst = append(dst, Span{int32(start), int32(j), String})
			}
			dst = append(dst, Span{int32(j), int32(j + n), Escape})
			j += n
			start = j
			continue
		}
		j++
	}
	if start < to {
		dst = append(dst, Span{int32(start), int32(to), String})
	}
	return dst
}

// appendChild scans src[from:to] with inner and appends its spans, shifted by
// from, to dst. A child region is a fresh expression: it carries no state into
// the next line.
func appendChild(dst []Span, inner *Language, src string, from, to int) []Span {
	prev := len(dst)
	dst, _ = inner.Line(dst, src[from:to], 0)
	for k := prev; k < len(dst); k++ {
		dst[k].Start += int32(from)
		dst[k].End += int32(from)
	}
	return dst
}

// appendMustaches appends src[from:to], a string interior that may contain
// {{ ... }} expressions: the text stays String, the delimiters are Punct and
// the expression is scanned as JavaScript.
func appendMustaches(dst []Span, src string, from, to int) []Span {
	seg := from
	for j := from; j+1 < to; {
		if src[j] == '{' && src[j+1] == '{' {
			if close := mustacheClose(src, j+2, to); close >= 0 {
				if seg < j {
					dst = append(dst, Span{int32(seg), int32(j), String})
				}
				dst = append(dst, Span{int32(j), int32(j + 2), Punct})
				dst = appendChild(dst, javascriptLang, src, j+2, close)
				dst = append(dst, Span{int32(close), int32(close + 2), Punct})
				j = close + 2
				seg = j
				continue
			}
		}
		j++
	}
	if seg < to {
		dst = append(dst, Span{int32(seg), int32(to), String})
	}
	return dst
}

// mustacheClose reports the index of the }} that closes src[from:limit), or -1.
func mustacheClose(src string, from, limit int) int {
	for j := from; j+1 < limit; j++ {
		if src[j] == '}' && src[j+1] == '}' {
			return j
		}
	}
	return -1
}

// vueAttrName reports whether the attribute name src[i:end] opens a Vue
// expression: a shorthand :, @ or # prefix, or a v- directive.
func vueAttrName(src string, i, end int) bool {
	if end-i >= 2 && src[i] == 'v' && src[i+1] == '-' {
		return true
	}
	if i > 0 {
		switch src[i-1] {
		case ':', '@', '#':
			return true
		}
	}
	return false
}

// classify names an identifier.
func (l *Language) classify(words map[string]Class, src string, start, end int) Class {
	word := src[start:end]
	if class, ok := words[word]; ok {
		return class
	}
	if !l.Hints {
		return Plain
	}
	// A name that opens a parenthesis is a call.
	for i := end; i < len(src); i++ {
		if src[i] == ' ' || src[i] == '\t' {
			continue
		}
		if src[i] == '(' {
			return Function
		}
		break
	}
	if end-start < 2 {
		return Plain
	}
	// A constant is shouted.
	shouted := true
	for i := 0; i < len(word); i++ {
		c := word[i]
		if c >= 'a' && c <= 'z' {
			shouted = false
			break
		}
	}
	if shouted {
		return Constant
	}
	// A type starts with a capital.
	if word[0] >= 'A' && word[0] <= 'Z' {
		return Type
	}
	return Plain
}

// keyFollows reports whether the next non-space byte is sep and does not open a
// doubled separator, so "a::b" stays a scope operator and not a key.
func keyFollows(src string, from int, sep byte) bool {
	for i := from; i < len(src); i++ {
		if src[i] == ' ' || src[i] == '\t' {
			continue
		}
		return src[i] == sep && (i+1 >= len(src) || src[i+1] != sep)
	}
	return false
}

// isDocBlock reports whether a block comment documents its subject: a doubled
// star or bang after the opener, and not the empty comment itself.
func isDocBlock(src, open, close string) bool {
	if len(src) <= len(open) {
		return false
	}
	// javadoc and go spell it "/**", elm and haskell "{-|".
	if c := src[len(open)]; c != '*' && c != '!' && c != '|' {
		return false
	}
	return !strings.HasPrefix(src[len(open):], close)
}

// markerAt reports whether one of markers starts at i.
func markerAt(src string, i int, markers []string) bool {
	for _, m := range markers {
		if len(src)-i >= len(m) && src[i:i+len(m)] == m {
			return true
		}
	}
	return false
}

// lineCommentAt reports whether a line comment starts at i. A hash only opens a
// comment at the start of a line or after a space, so a word like "a#b" stays
// an identifier.
func lineCommentAt(markers []string, src string, i int) bool {
	for _, m := range markers {
		if len(src)-i < len(m) || src[i:i+len(m)] != m {
			continue
		}
		if m[0] == '#' && i > 0 && !isSpaceByte(src[i-1]) {
			continue
		}
		return true
	}
	return false
}

// preprocDirective colours a preprocessor directive at src[i], a '#' that is
// the first non-blank byte of the line: the '#' and the name after it are the
// directive, and the angle-bracket operand of #include or #import is a string
// the quote rules cannot see. It reports where the ordinary scan resumes.
func preprocDirective(dst []Span, src string, i int) ([]Span, int) {
	n := len(src)
	dst = append(dst, Span{int32(i), int32(i + 1), Keyword})
	j := i + 1
	for j < n && isSpaceByte(src[j]) {
		j++
	}
	name := j
	for j < n && isIdentCont(src[j]) {
		j++
	}
	if j == name {
		// "# 1" is a line marker, not a directive: the '#' keeps its own
		// colour and the rest scans as code.
		return dst, i + 1
	}
	dst = append(dst, Span{int32(name), int32(j), Keyword})
	if !isInclude(src[name:j]) {
		return dst, j
	}
	for j < n && isSpaceByte(src[j]) {
		j++
	}
	if j < n && src[j] == '<' {
		if k := strings.IndexByte(src[j:], '>'); k >= 0 {
			dst = append(dst, Span{int32(j), int32(j + k + 1), String})
			return dst, j + k + 1
		}
	}
	return dst, j
}

// isInclude reports whether a directive name takes a header operand.
func isInclude(name string) bool {
	return name == "include" || name == "import"
}

// lineContinues reports whether a line ends with a backslash, which is how a
// preprocessor directive says the next line is still its own.
func lineContinues(src string) bool {
	i := len(src)
	for i > 0 && (src[i-1] == ' ' || src[i-1] == '\t' || src[i-1] == '\r') {
		i--
	}
	return i > 0 && src[i-1] == '\\'
}

// scanString scans from i for the close of rule, reporting where the string
// ends and whether it closed on this line.
func scanString(src string, i int, rule *StringRule, triple bool) (int, bool) {
	n := len(src)
	for j := i; j < n; {
		c := src[j]
		if rule.Pair {
			// nix spells both the delimiter and its escapes with '':
			// ''' is a quote and ''${ an interpolation, neither closes.
			if c == rule.Quote && j+1 < n && src[j+1] == rule.Quote {
				if j+2 < n && (src[j+2] == rule.Quote ||
					(src[j+2] == '$' && j+3 < n && src[j+3] == '{')) {
					j += 3
					continue
				}
				return j + 2, true
			}
			j++
			continue
		}
		if rule.Escape != 0 && c == rule.Escape && j+1 < n {
			j += 2
			continue
		}
		if c == rule.Quote {
			if !triple {
				return j + 1, true
			}
			if j+3 <= n && src[j+1] == rule.Quote && src[j+2] == rule.Quote {
				return j + 3, true
			}
			j++
			continue
		}
		j++
	}
	if rule.MultiLine || triple {
		return n, false
	}
	return n, true
}

// rawOpen recognises the raw string opener at src[i] (whose first byte is
// already known to be one the language uses). It reports the kind, the run or
// quote count of the terminator, where the body starts, and for C++ the
// delimiter, or ok false to fall through to the ordinary scanner.
func rawOpen(src string, i int, c byte) (kind RawKind, count, body int, delim string, ok bool) {
	n := len(src)
	switch c {
	case 'r': // Rust r"..." / r#"..."#
		j, hash := i+1, 0
		for j < n && src[j] == '#' && hash < 15 {
			j, hash = j+1, hash+1
		}
		if j < n && src[j] == '"' {
			return RawRust, hash, j + 1, "", true
		}
	case 'b': // Rust br"..." / br#"..."#
		if i+1 < n && src[i+1] == 'r' {
			j, hash := i+2, 0
			for j < n && src[j] == '#' && hash < 15 {
				j, hash = j+1, hash+1
			}
			if j < n && src[j] == '"' {
				return RawRust, hash, j + 1, "", true
			}
		}
	case '#': // Swift #"..."#, ##"..."## and so on
		j, hash := i, 0
		for j < n && src[j] == '#' && hash < 15 {
			j, hash = j+1, hash+1
		}
		if hash >= 1 && j < n && src[j] == '"' {
			return RawSwift, hash, j + 1, "", true
		}
	case '@': // C# @"..."
		if i+1 < n && src[i+1] == '"' {
			return RawCSharpVerbatim, 0, i + 2, "", true
		}
	case '"': // C# """...""" with three or more quotes
		// C# lets the opener be as long as it likes, but the closer only has
		// to reach the opener's run. Capping at three keeps an empty """"""
		// from needing a six-quote closer that never comes.
		j, q := i, 0
		for j < n && src[j] == '"' && q < 3 {
			j, q = j+1, q+1
		}
		if q >= 3 {
			return RawCSharpRaw, q, j, "", true
		}
	case '[': // Lua [==[ ... ]==]
		j, eq := i+1, 0
		for j < n && src[j] == '=' && eq < 15 {
			j, eq = j+1, eq+1
		}
		if j < n && src[j] == '[' {
			return RawLua, eq, j + 1, "", true
		}
	case 'R': // C++ R"delim( ... )delim"
		if i+1 < n && src[i+1] == '"' {
			if d, j, ok := scanCppDelim(src, i+2); ok {
				return RawCpp, 0, j + 1, d, true
			}
		}
	}
	return 0, 0, 0, "", false
}

// scanCppDelim reads the 0-16 character delimiter between R" and (.
func scanCppDelim(src string, i int) (string, int, bool) {
	j := i
	for j < len(src) && j-i < 16 && src[j] != '(' {
		c := src[j]
		if c == ' ' || c == ')' || c == '\\' || c < 0x20 || c == 0x7f {
			return "", 0, false
		}
		j++
	}
	if j >= len(src) || src[j] != '(' {
		return "", 0, false
	}
	return src[i:j], j, true
}

// scanRaw scans a raw string from the first body byte for its terminator.
func scanRaw(src string, i int, kind RawKind, count int, delim string) (int, bool) {
	n := len(src)
	switch kind {
	case RawCpp:
		// The closer is ")" plus the delimiter plus a quote.
		for j := i; j < n; j++ {
			if src[j] == ')' && j+len(delim)+1 < n && src[j+1+len(delim)] == '"' &&
				(len(delim) == 0 || src[j+1:j+1+len(delim)] == delim) {
				return j + 2 + len(delim), true
			}
		}
		return n, false
	case RawRust, RawSwift:
		// The closer is a quote followed by exactly count hashes.
		for j := i; j < n; j++ {
			if src[j] == '"' {
				k := j + 1
				for k < n && k-(j+1) < count && src[k] == '#' {
					k++
				}
				if k-(j+1) == count {
					return k, true
				}
			}
		}
		return n, false
	case RawCSharpVerbatim:
		// A doubled quote is an escaped quote; a lone quote closes. A verbatim
		// string may hold a newline, so an unclosed line stays open.
		for j := i; j < n; j++ {
			if src[j] == '"' {
				if j+1 < n && src[j+1] == '"' {
					j++
					continue
				}
				return j + 1, true
			}
		}
		return n, false
	case RawCSharpRaw:
		// The closer is exactly count quotes.
		for j := i; j < n; {
			if src[j] != '"' {
				j++
				continue
			}
			k := j
			for k < n && src[k] == '"' {
				k++
			}
			if k-j >= count {
				return k, true
			}
			j = k
		}
		return n, false
	case RawLua:
		// The closer is "]" plus exactly count "=" plus "]".
		for j := i; j < n; j++ {
			if src[j] != ']' {
				continue
			}
			k := j + 1
			for k < n && k-(j+1) < count && src[k] == '=' {
				k++
			}
			if k-(j+1) == count && k < n && src[k] == ']' {
				return k + 1, true
			}
		}
		return n, false
	}
	return n, false
}

// addDelim interns a C++ raw string delimiter in the builder's side table and
// returns the plus-one index, or zero when the table is full. Full means 31
// distinct multiline delimiters, which real code never reaches.
func addDelim(dels *[]string, d string) int {
	for i, e := range *dels {
		if e == d {
			return i + 1
		}
	}
	if len(*dels) >= 31 {
		return 0
	}
	*dels = append(*dels, d)
	return len(*dels)
}

// resDelim resolves a state delimiter index against the side table, degrading
// to an empty delimiter when the index is missing, which is what a direct Line
// caller without a builder gets after a multiline raw string.
func resDelim(dels *[]string, idx int) string {
	if idx <= 0 || idx > len(*dels) {
		return ""
	}
	return (*dels)[idx-1]
}

// scanBlockNested walks a block comment from the first body byte, counting
// open and close delimiters. It reports where the comment ends and, when the
// line ends first, the depth still open (saturating at 7; never negative).
func scanBlockNested(src string, i int, open, close string, depth int) (int, int) {
	n := len(src)
	d := min(max(depth, 1), 7)
	for i < n {
		switch {
		case strings.HasPrefix(src[i:], close):
			d--
			i += len(close)
			if d == 0 {
				return i, 0
			}
		case strings.HasPrefix(src[i:], open):
			d = min(d+1, 7)
			i += len(open)
		default:
			i++
		}
	}
	return n, d
}

// scanRegex scans a regex literal body from the byte after the opening slash,
// honouring backslash escapes and [...] classes. It requires the closing slash
// and its flags on the same line.
func scanRegex(src string, i int) (int, bool) {
	n := len(src)
	inClass := false
	for j := i; j < n; j++ {
		switch src[j] {
		case '\\':
			j++ // skip the escaped byte
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				k := j + 1
				for k < n && isASCIILetter(src[k]) {
					k++
				}
				return k, true
			}
		}
	}
	return 0, false
}

// isASCIILetter reports whether c is an unaccented letter.
func isASCIILetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// scanNumber scans a numeric literal: decimal, hexadecimal, or a float with a
// fraction and an exponent. lits adds the endings only one table has: a hash
// base and a sized width, which move where the literal stops.
func scanNumber(src string, i int, lits Literals) int {
	n := len(src)
	j := scanNumBody(src, i)
	if lits&LitBase != 0 && j < n && src[j] == '#' {
		// erlang's base literal, 16#FF and 2#1010: the digits after the
		// hash are the number's whatever base they spell, so the hash
		// does not end it.
		if k := j + 1; k < n && (isDigit(src[k]) || isASCIILetter(src[k])) {
			k++
			for k < n && (isDigit(src[k]) || isASCIILetter(src[k]) || src[k] == '_') {
				k++
			}
			return k
		}
	}
	if lits&LitSized != 0 && j < n && src[j] == '\'' {
		// verilog's sized literal: 8'hFF, 4'b1010.
		if k := scanSized(src, j); k > j {
			return k
		}
	}
	return j
}

// scanNumBody scans the forms the C family shares: a decimal or based integer,
// or a float with a fraction and an exponent. A hex float carries its own p
// exponent, which is how go spells 0x1p-2 and 0x1.fp+3.
func scanNumBody(src string, i int) int {
	n := len(src)
	if src[i] == '0' && i+1 < n {
		switch src[i+1] {
		case 'x', 'X':
			j := i + 2
			for j < n && (isHex(src[j]) || src[j] == '_') {
				j++
			}
			if j < n && src[j] == '.' && j+1 < n && isHex(src[j+1]) {
				j++
				for j < n && (isHex(src[j]) || src[j] == '_') {
					j++
				}
			}
			if j < n && (src[j] == 'p' || src[j] == 'P') {
				k := j + 1
				if k < n && (src[k] == '+' || src[k] == '-') {
					k++
				}
				if k < n && isDigit(src[k]) {
					for k < n && (isDigit(src[k]) || src[k] == '_') {
						k++
					}
					j = k
				}
			}
			return j
		case 'b', 'B', 'o', 'O':
			j := i + 2
			for j < n && (isHex(src[j]) || src[j] == '_') {
				j++
			}
			return j
		}
	}
	j := i
	for j < n && (isDigit(src[j]) || src[j] == '_') {
		j++
	}
	if j < n && src[j] == '.' && j+1 < n && isDigit(src[j+1]) {
		j++
		for j < n && (isDigit(src[j]) || src[j] == '_') {
			j++
		}
	}
	if j < n && (src[j] == 'e' || src[j] == 'E') {
		k := j + 1
		if k < n && (src[k] == '+' || src[k] == '-') {
			k++
		}
		if k < n && isDigit(src[k]) {
			for k < n && (isDigit(src[k]) || src[k] == '_') {
				k++
			}
			j = k
		}
	}
	return j
}

// scanSized scans verilog's sized literal from the quote in 8'hFF and 4'b1010,
// and the unbased form, '1 or 'z. It reports the end, or zero when the quote
// opens no literal, so an apostrophe in Verilog text still reads as one.
func scanSized(src string, i int) int {
	n := len(src)
	j := i + 1
	if j >= n {
		return 0
	}
	switch src[j] {
	case '0', '1', 'x', 'X', 'z', 'Z':
		return j + 1
	}
	if src[j] == 's' || src[j] == 'S' {
		j++
	}
	if j >= n {
		return 0
	}
	switch src[j] {
	case 'b', 'B', 'o', 'O', 'd', 'D', 'h', 'H':
	default:
		return 0
	}
	j++
	k := j
	for k < n && (isHex(src[k]) || src[k] == '_' || src[k] == 'x' || src[k] == 'X' ||
		src[k] == 'z' || src[k] == 'Z' || src[k] == '?') {
		k++
	}
	if k == j {
		// No digits: "'h" in prose is not a literal.
		return 0
	}
	return k
}

// scanChar scans the character literal at src[i], which haskell spells 'x' or
// '\n': one rune or one backslash escape between quotes. It reports the end, or
// zero when the quote closes none, which is what keeps foldl' and a Template
// Haskell 'Name as they were.
func scanChar(src string, i int) int {
	n := len(src)
	j := i + 1
	if j >= n {
		return 0
	}
	if src[j] == '\\' {
		j++
	}
	if j >= n {
		return 0
	}
	_, w := utf8.DecodeRuneInString(src[j:])
	j += w
	if j >= n || src[j] != '\'' {
		return 0
	}
	return j + 1
}

// scanDollarLit scans erlang's character literal at the $ in $a and $\n. It
// reports the end, or zero when the dollar stands alone.
func scanDollarLit(src string, i int) int {
	n := len(src)
	j := i + 1
	if j >= n {
		return 0
	}
	if src[j] == '\\' {
		j++
	}
	if j >= n {
		return 0
	}
	_, w := utf8.DecodeRuneInString(src[j:])
	return j + w
}

// scanVar scans the variable at the $ in src[i]: a name, or one of the special
// parameters a shell and awk spell with a sigil and a punctuation byte, which is
// what $1, $? and $# are. It reports the end, or zero when the dollar stands
// alone, so $(cmd) and ${VAR} keep their own scan.
func scanVar(src string, i int) int {
	if i+1 >= len(src) {
		return 0
	}
	c := src[i+1]
	if isIdentStart(c) {
		return scanIdent(src, i+1, false)
	}
	if isDigit(c) {
		return i + 2
	}
	switch c {
	case '#', '?', '@', '*', '$', '!', '-':
		return i + 2
	}
	return 0
}

// interpOpen recognises a C# interpolated string prefix at src[i], where the
// quote is preceded by "$", "$@" or "@$". It reports where the body starts and
// whether that body is verbatim. A raw string's three quotes are left to the raw
// rule, whose opener stays in charge of it.
func interpOpen(src string, i int) (body int, verbatim, ok bool) {
	n := len(src)
	j := i
	switch src[i] {
	case '$':
		j++
		if j < n && src[j] == '@' {
			j++
			verbatim = true
		}
	case '@':
		if i+1 >= n || src[i+1] != '$' {
			return 0, false, false
		}
		j, verbatim = i+2, true
	default:
		return 0, false, false
	}
	if j >= n || src[j] != '"' {
		return 0, false, false
	}
	if !verbatim && j+2 < n && src[j+1] == '"' && src[j+2] == '"' {
		return 0, false, false
	}
	return j + 1, verbatim, true
}

// scanIdent scans an identifier, taking any byte above ASCII as part of it so
// a non-ASCII name is one token without decoding it. With hyphen, a hyphen
// followed by a letter continues the name, which is how "background-color" and
// "runs-on" stay one token while "a - b" stays three.
func scanIdent(src string, i int, hyphen bool) int {
	n := len(src)
	j := i + 1
	for j < n {
		if hyphen && src[j] == '-' && j+1 < n && isIdentStart(src[j+1]) {
			j++
			continue
		}
		if !isIdentCont(src[j]) {
			break
		}
		j++
	}
	return j
}

// isDigit reports whether c is a decimal digit.
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// isHex reports whether c is a hexadecimal digit.
func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// isSpaceByte reports whether c is a space or a tab.
func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' }

// isIdentStart reports whether c can start an identifier.
func isIdentStart(c byte) bool {
	return c == '_' || c >= 0x80 ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isIdentCont reports whether c can continue an identifier.
func isIdentCont(c byte) bool { return isIdentStart(c) || isDigit(c) }

// isOp reports whether c is an operator byte. Runs of them are one token, so
// ":=", "==" and "->" each paint as a single span.
func isOp(c byte) bool {
	switch c {
	case '+', '-', '*', '/', '%', '=', '<', '>', '!', '&', '|', '^', '~', ':', '?':
		return true
	}
	return false
}

// isPunct reports whether c is a delimiter or separator.
func isPunct(c byte) bool {
	switch c {
	case '(', ')', '{', '}', '[', ']', ',', ';', '.':
		return true
	}
	return false
}
