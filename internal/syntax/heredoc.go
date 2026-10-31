package syntax

// Heredocs are line-based literals: a "<<WORD" opener, a body of raw lines,
// and a terminator line. This package treats the body and the terminator as
// one String span per line, so a shell script's embedded SQL stops reading as
// shell keywords and its terminator stops reading as a type.
//
// A heredoc is not a token; it is scanner state. The opener is found while a
// line is scanned (the scanner is where the "<<" is known to be an operator
// and not part of a string), and the State's stateHeredoc bit then says the
// next line is a heredoc line. The terminator word lives in a small
// per-Builder table, because it is too long for the State; the table is also
// the queue of open heredocs, so the front slot is always q[0]. Eight slots
// and a 24-byte word cap keep a side to a couple of hundred bytes, allocated
// once with the Builder rather than per line.

const (
	heredocSlots   = 8 // one Builder side holds this many open heredocs
	heredocWordMax = 24
)

// heredocKind is how a language spells a here-document. It is zero for
// languages without one, which keeps the scanner's only cost one flag test.
type heredocKind uint8

const (
	hdNone  heredocKind = iota
	hdShell             // <<WORD, <<-WORD, <<'WORD', <<"WORD", <<\WORD
	hdRuby              // <<WORD, <<-WORD, <<~WORD, <<'WORD', <<"WORD"
	hdPerl              // <<WORD, <<'WORD', <<"WORD", <<~WORD
	hdPHP               // <<<WORD, <<<'WORD'
	hdHCL               // <<WORD, <<-WORD; no quoting, the word is bare
)

// Slot flags carried with a terminator.
const (
	heredocFlagIndent uint8 = 1 << 0 // the terminator line may be indented
	heredocFlagTab    uint8 = 1 << 1 // only tabs may indent it (shell "<<-")
	heredocFlagWord   uint8 = 1 << 2 // php: the word closes on a non-word byte
)

// heredocSlot is one open heredoc: its terminator and how to match it.
type heredocSlot struct {
	word  [heredocWordMax]byte
	n     uint8
	flags uint8
}

// heredocSet is one Builder side's open heredocs. The queue records the order
// they opened in - heredocs deliver their bodies first-opened first, so a line
// like "cat <<A <<B" hands the next lines to A and only then to B. The bitmap
// tracks which slots are taken, and every field here is a value, so the set
// costs no allocation beyond the Builder that owns it.
type heredocSet struct {
	slots [heredocSlots]heredocSlot
	q     [heredocSlots]uint8 // open slots, earliest first
	n     uint8               // how many are open
	used  uint8               // bitmap of taken slots
}

// open records a new heredoc and returns its slot, or -1 when the table is
// full and the opener should fall back to plain-code behaviour.
func (h *heredocSet) open(word string, flags uint8) int {
	if h.n >= heredocSlots || len(word) > heredocWordMax {
		return -1
	}
	var i uint8
	for i = 0; i < heredocSlots; i++ {
		if h.used&(1<<i) == 0 {
			break
		}
	}
	if i == heredocSlots {
		return -1
	}
	h.used |= 1 << i
	s := &h.slots[i]
	s.n = uint8(len(word))
	s.flags = flags
	copy(s.word[:], word)
	h.q[h.n] = i
	h.n++
	return int(i)
}

// has reports whether slot is one of the open heredocs.
func (h *heredocSet) has(slot int) bool {
	return slot >= 0 && slot < heredocSlots && h.used&(1<<uint(slot)) != 0
}

// close removes slot, which is always the front of the queue: a terminator
// line ends the earliest heredoc still open.
func (h *heredocSet) close(slot int) {
	if !h.has(slot) || h.n == 0 {
		return
	}
	h.used &^= 1 << uint(slot)
	for j := uint8(0); j < h.n; j++ {
		if h.q[j] == uint8(slot) {
			copy(h.q[j:h.n-1], h.q[j+1:h.n])
			h.n--
			return
		}
	}
}

// line tokenises one line whose State says a heredoc is open. The body is one
// String span covering the whole line; the terminator line is String too, so
// the entire literal reads as one block and the closing word never reads as a
// type or an identifier. The front of the queue is the heredoc the line
// belongs to.
func (h *heredocSet) line(dst []Span, src string, st State) ([]Span, State) {
	if h.n == 0 {
		// A stale flag can only follow a table reuse; colour nothing and
		// close it out.
		return dst, st &^ stateHeredoc
	}
	s := &h.slots[h.q[0]]
	if len(src) > 0 {
		dst = append(dst, Span{0, int32(len(src)), String})
	}
	if s.closes(src) {
		h.close(int(h.q[0]))
		if h.n > 0 {
			return dst, stateHeredoc
		}
		return dst, 0
	}
	return dst, st
}

// closes reports whether src is the line that ends this slot's heredoc.
func (h *heredocSlot) closes(src string) bool {
	s := src
	if h.flags&heredocFlagIndent != 0 {
		if h.flags&heredocFlagTab != 0 {
			for len(s) > 0 && s[0] == '\t' {
				s = s[1:]
			}
		} else {
			for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
				s = s[1:]
			}
		}
	}
	w := h.word[:int(h.n)]
	if h.flags&heredocFlagWord != 0 {
		// php closes on the word followed by a non-word byte, so "EOT;"
		// ends the heredoc and "EOTX" is another body line.
		if len(s) < len(w) {
			return false
		}
		for i := range w {
			if s[i] != w[i] {
				return false
			}
		}
		return len(s) == len(w) || !isWordByte(s[len(w)])
	}
	if len(s) != len(w) {
		return false
	}
	for i := range w {
		if s[i] != w[i] {
			return false
		}
	}
	return true
}

// isWordByte reports whether c is a word character as a heredoc sees it: an
// ASCII letter, digit or underscore. A byte above ASCII that starts a language
// identifier is not a word byte to a shell, so it does not guard here.
func isWordByte(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

// isLetter reports whether c starts a bare heredoc word.
func isLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// parseHeredoc reads a heredoc opener at src[i], which is a "<" the language
// spells a heredoc with. It returns the terminator word, its flags, where the
// operator and word spans fall, and the offset just past the opener. ok is
// false when this "<" is not an opener - an append or shift glued to a word or
// a variable, or a word that does not start with a letter. Requiring the next
// byte to be a letter and the previous byte to not be a word byte or "$" was
// measured to cut the false-positive "<<"s to zero on a corpus of 3621 real
// heredocs without losing any true ones.
func parseHeredoc(kind heredocKind, src string, i int) (word string, flags uint8, opEnd, strStart, strEnd int, ok bool) {
	n := len(src)
	if i > 0 {
		if p := src[i-1]; p == '$' || isWordByte(p) {
			return "", 0, 0, 0, 0, false
		}
	}
	// php spells it with three "<"; the rest with two.
	op := 2
	if kind == hdPHP {
		if i+2 >= n || src[i+1] != '<' || src[i+2] != '<' {
			return "", 0, 0, 0, 0, false
		}
		op = 3
		flags |= heredocFlagIndent | heredocFlagWord
	} else if i+1 >= n || src[i+1] != '<' {
		return "", 0, 0, 0, 0, false
	}
	j := i + op
	switch kind {
	case hdShell:
		if j < n && src[j] == '-' {
			flags |= heredocFlagIndent | heredocFlagTab
			j++
		}
	case hdRuby:
		if j < n && (src[j] == '-' || src[j] == '~') {
			flags |= heredocFlagIndent
			j++
		}
	case hdPerl:
		if j < n && src[j] == '~' {
			flags |= heredocFlagIndent
			j++
		}
	case hdHCL:
		// HCL strips the common indentation itself, so <<- allows any
		// whitespace before the terminator, not only tabs as shell does.
		if j < n && src[j] == '-' {
			flags |= heredocFlagIndent
			j++
		}
	}
	if j >= n {
		return "", 0, 0, 0, 0, false
	}
	opEnd = j
	quot := byte(0)
	switch src[j] {
	case '\'', '"':
		if kind == hdHCL {
			// HCL's word is bare, so a quote here is not an opener.
			return "", 0, 0, 0, 0, false
		}
		quot = src[j]
		strStart = j
		j++
	case '\\':
		if kind != hdShell {
			return "", 0, 0, 0, 0, false
		}
		// <<\EOF quotes the word; the backslash belongs to the operator.
		j++
		opEnd = j
		strStart = j
	default:
		strStart = j
	}
	if j >= n {
		return "", 0, 0, 0, 0, false
	}
	if quot == 0 && !isLetter(src[j]) {
		return "", 0, 0, 0, 0, false
	}
	if quot != 0 {
		for j < n && src[j] != quot {
			j++
		}
		if j >= n {
			return "", 0, 0, 0, 0, false
		}
		word = src[strStart+1 : j]
		strEnd = j + 1
	} else {
		for j < n && isWordByte(src[j]) {
			j++
		}
		word = src[strStart:j]
		strEnd = j
	}
	if len(word) == 0 {
		return "", 0, 0, 0, 0, false
	}
	return word, flags, opEnd, strStart, strEnd, true
}
