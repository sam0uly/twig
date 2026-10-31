package syntax

// JSX is not an embedded language: it is JavaScript with a second surface
// syntax, and the markup inside it is the same scanner in another mode. So the
// machine here is four modes and a depth, carried in State, and the "<" that
// opens a tag is recognised by one rule:
//
//	"<" opens a tag when the previous thing on the line lets an expression
//	start, the next byte names a tag (a letter, "_", "$", ">" or "/"), and
//	it is not the start of a type parameter list ("<T," or "<T extends").
//
// That rule was measured against tree-sitter's own parse of React and Headless
// UI: 0.09% and 0.41% of tag decisions wrong, against 100% today. Requiring a
// closing ">" on the same line reads better but is worse — 2% and 6% — because
// a tag's attributes are usually on the following lines.

// jsxAllowed reports whether src[i] is a "<" that opens a JSX tag.
func jsxAllowed(src string, i int, st State) bool {
	if i+1 >= len(src) {
		return false
	}
	if !exprOKAt(src, i, st) {
		return false
	}
	c := src[i+1]
	switch {
	case c == '>', c == '/': // a fragment, or a closing tag
		return true
	case isIdentStart(c):
		return !isGenericOpen(src, i)
	}
	return false
}

// isGenericOpen reports whether "<" opens a type parameter list rather than a
// tag: "<T," is the trailing-comma form of a generic arrow, and "<T extends" is
// a constrained one.
func isGenericOpen(src string, i int) bool {
	j := i + 1
	if j >= len(src) || !isIdentStart(src[j]) {
		return false
	}
	for j < len(src) && isIdentCont(src[j]) {
		j++
	}
	for j < len(src) && isSpaceByte(src[j]) {
		j++
	}
	if j < len(src) && src[j] == ',' {
		return true
	}
	const ext = "extends"
	if j+len(ext) <= len(src) && src[j:j+len(ext)] == ext {
		after := j + len(ext)
		return after >= len(src) || !isIdentCont(src[after])
	}
	return false
}

// exprOKAt reports whether an expression may start at i, which is what makes a
// "<" there a tag rather than a comparison. It reads the byte before the
// position, and falls back to the bit saved at the end of the previous line.
func exprOKAt(src string, i int, st State) bool {
	for j := i - 1; j >= 0; j-- {
		if isSpaceByte(src[j]) || src[j] == '\r' {
			continue
		}
		switch src[j] {
		case '=', '(', ',', ':', '?', '&', '|', '!', ';', '{', '}', '[', '+', '-', '*', '%', '>', '<':
			return true
		}
		return false
	}
	return st&stateExpr != 0
}

// exprOKAfter reports whether the next line may start with an expression.
func exprOKAfter(src string) bool {
	for j := len(src) - 1; j >= 0; j-- {
		if isSpaceByte(src[j]) || src[j] == '\r' {
			continue
		}
		switch src[j] {
		case '=', '(', ',', ':', '?', '&', '|', '!', ';', '{', '[', '+', '-', '*', '%', '>':
			return true
		}
		return false
	}
	return false
}

// lineJSX scans a line that starts inside a tag or between elements.
func (l *Language) lineJSX(dst []Span, src string, st State, dels *[]string) ([]Span, State) {
	switch st.mode() {
	case modeExpr:
		// An expression is JavaScript: hand it to the ordinary scanner, which
		// returns to JSX when the brace that opened it closes.
		return l.lineJS(dst, src, 0, st, dels, nil)
	case modeTag:
		return l.lineTag(dst, src, 0, st, dels)
	default:
		return l.lineChildren(dst, src, 0, st, dels)
	}
}

// lineTag scans the attributes of an open tag from i, and finishes the tag.
func (l *Language) lineTag(dst []Span, src string, i int, st State, dels *[]string) ([]Span, State) {
	n := len(src)
	p := l.prepared()
	for i < n {
		c := src[i]
		switch {
		case isSpaceByte(c) || c == '\r':
			i++
		case c == '>':
			if st.closing() {
				return l.continueAt(dst, src, i+1, popElement(st), dels)
			}
			return l.continueAt(dst, src, i+1, finishTag(st), dels)
		case c == '/' && i+1 < n && src[i+1] == '>':
			return l.continueAt(dst, src, i+2, popElement(st), dels)
		case c == '{':
			dst = append(dst, Span{int32(i), int32(i + 1), Punct})
			next := st.withMode(modeExpr).withBrace(0) | stateReturn
			return l.lineJS(dst, src, i+1, next, dels, nil)
		case c == '"' || c == '\'':
			rule := StringRule{Quote: c, Escape: '\\'}
			end, _ := scanString(src, i+1, &rule, false)
			dst = appendString(dst, src, i, end, &rule)
			i = end
		case c == '=':
			dst = append(dst, Span{int32(i), int32(i + 1), Operator})
			i++
		case isIdentStart(c):
			end := scanIdent(src, i, p.hyphen)
			dst = append(dst, Span{int32(i), int32(end), Key})
			i = end
		default:
			i++
		}
	}
	return dst, st
}

// continueAt scans the rest of a line from i in whatever mode the state is in.
func (l *Language) continueAt(dst []Span, src string, i int, st State, dels *[]string) ([]Span, State) {
	switch st.mode() {
	case modeTag:
		return l.lineTag(dst, src, i, st, dels)
	case modeChildren:
		return l.lineChildren(dst, src, i, st, dels)
	default:
		return l.lineJS(dst, src, i, st, dels, nil)
	}
}

// finishTag applies the ">" of an opening tag and descends into its children.
func finishTag(st State) State {
	return st.withMode(modeChildren) &^ stateReturn
}

// popElement closes the innermost element.
func popElement(st State) State {
	d := st.depth() - 1
	if d < 0 {
		d = 0
	}
	st = st.withDepth(d).withBrace(0) &^ stateReturn
	if d == 0 {
		return st.withMode(modeJS)
	}
	return st.withMode(modeChildren)
}

// lineChildren scans between elements from i: text, expressions and tags.
func (l *Language) lineChildren(dst []Span, src string, i int, st State, dels *[]string) ([]Span, State) {
	n := len(src)
	for i < n {
		switch src[i] {
		case '{':
			dst = append(dst, Span{int32(i), int32(i + 1), Punct})
			next := st.withMode(modeExpr).withBrace(0) &^ stateReturn
			return l.lineJS(dst, src, i+1, next, dels, nil)
		case '<':
			var ok bool
			var next State
			dst, i, next, ok = l.openTag(dst, src, i, st)
			if !ok {
				i++
				continue
			}
			st = next
			switch st.mode() {
			case modeTag:
				return l.lineTag(dst, src, i, st, dels)
			case modeChildren:
				continue
			default:
				return l.lineJS(dst, src, i, st, dels, nil)
			}
		default:
			i++
		}
	}
	return dst, st.withExprBit(src)
}

// withExprBit records whether the next line may start an expression.
func (s State) withExprBit(src string) State {
	if exprOKAfter(src) {
		return s | stateExpr
	}
	return s &^ stateExpr
}

// openTag handles a "<": a closing tag, a fragment or a new element. ok is false
// when the "<" is just text or a comparison.
func (l *Language) openTag(dst []Span, src string, i int, st State) ([]Span, int, State, bool) {
	n := len(src)
	j := i + 1
	if j >= n {
		return dst, i, st, false
	}
	closing := false
	if src[j] == '/' {
		closing = true
		j++
	}
	if j >= n {
		// "</" ended the line: a closing tag split across a line boundary.
		// There is no name and no ">" to read, so treat the "<" as an
		// operator and let the next line pick the tag up fresh instead of
		// indexing past the end.
		return dst, i, st, false
	}
	if src[j] == '>' { // a fragment, <> or </>
		if closing {
			return dst, j + 1, popElement(st), true
		}
		return dst, j + 1, st.withDepth(st.depth() + 1).withMode(modeChildren), true
	}
	if !isIdentStart(src[j]) {
		return dst, i, st, false
	}
	end := scanIdent(src, j, true)
	// A dotted name, <Foo.Bar>, stays one tag token.
	for end+1 < n && src[end] == '.' && isIdentStart(src[end+1]) {
		end = scanIdent(src, end+1, true)
	}
	dst = append(dst, Span{int32(j), int32(end), Tag})
	next := st
	if closing {
		next |= stateReturn
	} else if st.depth() == 0 {
		next = next.withDepth(1)
	}
	return dst, end, next.withMode(modeTag), true
}
