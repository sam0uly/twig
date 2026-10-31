package syntax

// Index holds one file's tokens keyed by source line and side. A rendered row
// names its source lines in diff.Line.Old and diff.Line.New, so one index
// serves the unified view, the split view and the all-changes view without the
// renderer knowing which file or block the row came from.
//
// Tokens are packed into 32 bits: twelve for the start offset, twelve for the
// length and eight for the class. Twelve bits caps a token at 4095 bytes and a
// line at 4095 bytes, which no source line in a diff reaches; past that the
// rest of the line stays plain rather than costing a wider word.
type Index struct {
	lang *Language
	toks []uint32
	old  side
	new  side
}

// side is the token run of every source line on one side of a diff.
type side struct {
	off []int32  // off[line-1] is the index of the line's first token
	num []uint16 // how many tokens the line has
}

// Token encoding limits.
const (
	tokenOffsetBits = 12
	tokenLenBits    = 12
	tokenClassBits  = 8
	tokenMax        = 1<<tokenOffsetBits - 1
	tokenMaxLen     = 1<<tokenLenBits - 1
	tokenClassMask  = 1<<tokenClassBits - 1
)

// packSpan encodes a span as one token word.
func packSpan(s Span) (uint32, bool) {
	if s.Start < 0 || s.Start > tokenMax || s.End <= s.Start || s.End > tokenMax {
		return 0, false
	}
	return uint32(s.Start)<<(tokenLenBits+tokenClassBits) |
		uint32(s.End-s.Start)<<tokenClassBits |
		uint32(s.Class)&tokenClassMask, true
}

// UnpackToken returns the source offsets and class of one packed token.
func UnpackToken(w uint32) (start, end int32, class Class) {
	start = int32(w >> (tokenLenBits + tokenClassBits))
	length := int32((w >> tokenClassBits) & tokenMaxLen)
	return start, start + length, Class(w & tokenClassMask)
}

// Old returns the tokens of one old-side source line, numbered from one.
func (x *Index) Old(line int) []uint32 {
	if x == nil {
		return nil
	}
	return x.old.run(x.toks, line)
}

// New returns the tokens of one new-side source line, numbered from one.
func (x *Index) New(line int) []uint32 {
	if x == nil {
		return nil
	}
	return x.new.run(x.toks, line)
}

// Language is the language the index was built for.
func (x *Index) Language() *Language {
	if x == nil {
		return nil
	}
	return x.lang
}

// Bytes is the memory the index keeps resident.
func (x *Index) Bytes() int {
	if x == nil {
		return 0
	}
	return 4*len(x.toks) + 4*cap(x.old.off) + 2*cap(x.old.num) +
		4*cap(x.new.off) + 2*cap(x.new.num) + 16
}

// run returns the tokens of line, or nil when the line was never tokenised.
func (s *side) run(toks []uint32, line int) []uint32 {
	if line <= 0 || line > len(s.off) {
		return nil
	}
	start := s.off[line-1]
	n := int(s.num[line-1])
	if n == 0 {
		return nil
	}
	return toks[start : int(start)+n]
}

// ensure grows the side to hold line.
func (s *side) ensure(line int) {
	for len(s.off) < line {
		s.off = append(s.off, 0)
		s.num = append(s.num, 0)
	}
}

// set records where one line's tokens are.
func (s *side) set(line, start, n int) {
	if line <= 0 {
		return
	}
	s.ensure(line)
	s.off[line-1] = int32(start)
	s.num[line-1] = uint16(min(n, 0xffff))
}

// Builder accumulates the tokens of one file, one source line at a time. It
// keeps the scanner state of each side across calls, which is what lets a
// block comment opened inside a hunk colour the lines that follow it.
type Builder struct {
	lang *Language
	idx  *Index
	buf  []Span
	old  State
	new  State
	// oldHD and newHD are each side's open heredocs. They live here rather
	// than on the shared Language or in a per-line struct: the terminators
	// are file-specific, and a per-line table cost ~128ns/line in measurement.
	oldHD heredocSet
	newHD heredocSet
	// oldIn and newIn hold the embedded region each side is inside, if any:
	// an HTML <script> body is scanned by the JavaScript table, not by HTML.
	oldIn embedState
	newIn embedState
	// dels is the side table of C++ raw string delimiters open across a line
	// boundary; the scanner state keeps only the index.
	dels []string
}

// embedState is an open embedded region on one side. The inner language keeps
// its own state so the host's survives the region untouched.
type embedState struct {
	lang  *Language
	close string
	fc    byte // fence char, zero unless the region is a fenced code block
	fn    int  // shortest run of fc that closes the fence
	state State
}

// NewBuilder starts an index for one language.
func NewBuilder(lang *Language) *Builder {
	if lang == nil {
		return nil
	}
	return &Builder{lang: lang, idx: &Index{lang: lang}}
}

// Old tokenises one old-side line of src, without the diff sign prefix.
func (b *Builder) Old(src string, line int) {
	if b == nil {
		return
	}
	start := len(b.idx.toks)
	b.buf, b.old = b.scan(b.buf[:0], src, b.old, &b.oldIn, &b.oldHD)
	b.idx.old.set(line, start, b.emit())
}

// New tokenises one new-side line of src, without the diff sign prefix.
func (b *Builder) New(src string, line int) {
	if b == nil {
		return
	}
	start := len(b.idx.toks)
	b.buf, b.new = b.scan(b.buf[:0], src, b.new, &b.newIn, &b.newHD)
	b.idx.new.set(line, start, b.emit())
}

// Context tokenises one unchanged line, which belongs to both sides. The sides
// usually agree, and then one run of tokens serves both.
func (b *Builder) Context(src string, oldLine, newLine int) {
	if b == nil {
		return
	}
	oldState, newState := b.old, b.new

	start := len(b.idx.toks)
	b.buf, b.old = b.scan(b.buf[:0], src, oldState, &b.oldIn, &b.oldHD)
	n := b.emit()
	b.idx.old.set(oldLine, start, n)

	if newState == oldState && b.oldIn == b.newIn && !oldState.inHeredoc() {
		b.new = b.old
		b.newHD = b.oldHD
		b.newIn = b.oldIn
		b.idx.new.set(newLine, start, n)
		return
	}
	// The sides disagreed, which happens when a hunk opens inside a comment
	// or a string on one side only. Colour the new side from its own state.
	newStart := len(b.idx.toks)
	b.buf, b.new = b.scan(b.buf[:0], src, newState, &b.newIn, &b.newHD)
	b.idx.new.set(newLine, newStart, b.emit())
}

// scan tokenises one line with whichever table owns it: the embedded one while
// a region is open, the host otherwise. A fence or the host close wins, so the
// line that ends </script> or a code fence is scanned as the host and the
// inner state is dropped with the region.
func (b *Builder) scan(dst []Span, src string, st State, in *embedState, hds *heredocSet) ([]Span, State) {
	if hds != nil && st.inHeredoc() {
		return hds.line(dst, src, st)
	}
	host := b.lang
	if in.lang != nil || in.fc != 0 {
		if in.fc != 0 {
			if !fenceCloses(src, in.fc, in.fn) {
				if in.lang != nil {
					dst, in.state = in.lang.line(dst, src, in.state, &b.dels)
				}
				return dst, st
			}
			// A fence closer is never an opener, so this line belongs to the
			// host and the body ends with it.
			*in = embedState{}
			dst, st = host.scanLine(dst, src, st, &b.dels, hds)
			return dst, st
		}
		if !closesAfter(src, 0, in.close) {
			dst, in.state = in.lang.line(dst, src, in.state, &b.dels)
			return dst, st
		}
		// The host close ends the region: this line belongs to the host, and the
		// inner state dies with the region.
		*in = embedState{}
	}
	// A region may open on this line. The opener line itself is host text and
	// the body starts on the next one, unless the same line closes the region
	// again: `<script src="app.js"></script>` is the commonest line of HTML
	// there is, and leaving it open would cost the rest of the file.
	if host != nil && len(host.Embed) > 0 {
		for i := range host.Embed {
			e := &host.Embed[i]
			picked, fc, fn, at := e.match(src)
			if picked == nil && fc == 0 {
				continue
			}
			dst, st = host.scanLine(dst, src, st, &b.dels, hds)
			if fc == 0 && (closesAfter(src, at+len(e.Open), e.Close) || selfClosing(src, at)) {
				return dst, st
			}
			*in = embedState{lang: picked, close: e.Close, fc: fc, fn: fn}
			return dst, st
		}
	}
	dst, st = host.scanLine(dst, src, st, &b.dels, hds)
	return dst, st
}

// match reports the language a region this line opens, or nil, and where the
// opener sits. For a fence it also reports the fence char and the run length
// that opens, and must close it.
func (e *Embed) match(src string) (*Language, byte, int, int) {
	if e.Fence {
		if ch, run, tag, ok := fenceOpen(src); ok {
			return fenceTag(tag), ch, run, 0
		}
		return nil, 0, 0, 0
	}
	at := openAt(src, e.Open)
	if at < 0 {
		return nil, 0, 0, 0
	}
	if e.Attr != "" && len(e.ByAttr) > 0 {
		if v, ok := attrValue(src, e.Attr); ok {
			if l, ok := e.ByAttr[v]; ok {
				return l, 0, 0, at
			}
		}
	}
	return e.Lang, 0, 0, at
}

// openAt is where an embed's opener starts, or -1. Markup opens the same region
// in any case, and `<scripting` opens none, so the byte after the opener must
// not carry the name on.
func openAt(src, open string) int {
	if open == "" {
		return -1
	}
	for i := 0; i+len(open) <= len(src); i++ {
		if !foldAt(src, i, open) {
			continue
		}
		if j := i + len(open); j == len(src) || !nameByte(src[j]) {
			return i
		}
	}
	return -1
}

// selfClosing reports whether the tag starting at off ends with "/>", which
// makes it an element with no body: it opens no region, however long the file
// runs on. It reads back from the first ">" so `<script />` and `<script/>`
// both count.
func selfClosing(src string, off int) bool {
	if off < 0 {
		return false
	}
	end := off
	for end < len(src) && src[end] != '>' {
		end++
	}
	if end >= len(src) {
		return false
	}
	for end > off && blank(src[end-1]) {
		end--
	}
	return end > off && src[end-1] == '/'
}

// closesAfter reports whether a region closes at or after off, ignoring case:
// markup ends `</script>` and `</SCRIPT>` alike.
func closesAfter(src string, off int, close string) bool {
	if close == "" || off < 0 {
		return false
	}
	for i := off; i+len(close) <= len(src); i++ {
		if foldAt(src, i, close) {
			return true
		}
	}
	return false
}

// foldAt reports whether src has want at i, comparing ASCII case-insensitively.
func foldAt(src string, i int, want string) bool {
	if i < 0 || i+len(want) > len(src) {
		return false
	}
	for j := range len(want) {
		if lowerASCII(src[i+j]) != lowerASCII(want[j]) {
			return false
		}
	}
	return true
}

// lowerASCII folds one ASCII letter to lower case, leaving every other byte.
func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

// nameByte reports whether c continues a tag or attribute name, which is how a
// longer word after an opener (`<scripting`) is told from a real region.
func nameByte(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' ||
		'0' <= c && c <= '9' || c == '_' || c == '-'
}

// fenceOpen reports whether src opens a fenced code block: three or more
// backticks or tildes as the first non-blank bytes. It returns the fence char,
// the run length that a closer must reach, and the info tag, if any.
func fenceOpen(src string) (ch byte, run int, tag string, ok bool) {
	i := 0
	for i < len(src) && blank(src[i]) {
		i++
	}
	if i >= len(src) {
		return 0, 0, "", false
	}
	ch = src[i]
	if ch != '`' && ch != '~' {
		return 0, 0, "", false
	}
	j := i
	for j < len(src) && src[j] == ch {
		j++
	}
	if j-i < 3 {
		return 0, 0, "", false
	}
	run = j - i
	for j < len(src) && blank(src[j]) {
		j++
	}
	tagStart := j
	for j < len(src) && !blank(src[j]) {
		j++
	}
	return ch, run, src[tagStart:j], true
}

// blank reports whether c is whitespace at a fence boundary.
func blank(c byte) bool { return c == ' ' || c == '\t' || c == '\r' }

// fenceCloses reports whether src ends a fence opened with min of ch: its
// first non-blank run is at least min of ch and the rest of the line is blank.
func fenceCloses(src string, ch byte, min int) bool {
	i := 0
	for i < len(src) && blank(src[i]) {
		i++
	}
	j := i
	for j < len(src) && src[j] == ch {
		j++
	}
	if j-i < min {
		return false
	}
	for ; j < len(src); j++ {
		if !blank(src[j]) {
			return false
		}
	}
	return true
}

// attrValue reads attr="value" or attr='value' from a tag line.
func attrValue(src, attr string) (string, bool) {
	for i := 0; i+len(attr) < len(src); i++ {
		if src[i:i+len(attr)] != attr {
			continue
		}
		if i > 0 && isIdentCont(src[i-1]) {
			continue
		}
		j := i + len(attr)
		for j < len(src) && src[j] == ' ' {
			j++
		}
		if j >= len(src) || src[j] != '=' {
			continue
		}
		j++
		for j < len(src) && src[j] == ' ' {
			j++
		}
		if j >= len(src) || (src[j] != '"' && src[j] != '\'') {
			continue
		}
		q := src[j]
		end := j + 1
		for end < len(src) && src[end] != q {
			end++
		}
		return src[j+1 : end], true
	}
	return "", false
}

// emit packs the last tokenised line onto the index and returns how many
// tokens it added.
func (b *Builder) emit() int {
	start := len(b.idx.toks)
	for _, s := range b.buf {
		if w, ok := packSpan(s); ok {
			b.idx.toks = append(b.idx.toks, w)
		}
	}
	return len(b.idx.toks) - start
}

// Index finishes the build and returns the tokens.
func (b *Builder) Index() *Index {
	if b == nil {
		return nil
	}
	b.buf = nil
	return b.idx
}
