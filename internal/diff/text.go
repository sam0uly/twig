package diff

import (
	"math"
	"sort"
	"strings"
)

// text is one side of a diff: the content plus the start offset of every
// line. Indexing offsets instead of splitting into a []string keeps a
// multi-million line file at four bytes a line instead of a string header
// each, which is most of the difference between a diff that fits in memory
// and one that does not.
type text struct {
	s     string
	offs  []int32
	lines []string // fallback for content too large to index with int32
}

// maxTextIndex is the largest content length an int32 offset can address.
const maxTextIndex = math.MaxInt32

// indexText indexes the lines of s. It produces exactly the lines splitLines
// would, including the trimming of the terminator.
func indexText(s string) text {
	if s == "" {
		return text{}
	}
	if len(s) > maxTextIndex {
		return text{s: s, lines: splitLines(s)}
	}

	// Offsets of the bytes that start a line: the first byte, plus every byte
	// after a newline that is not the content's final terminator.
	offs := make([]int32, 1, 1+strings.Count(s, "\n"))
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '\n' {
			offs = append(offs, int32(i+1))
		}
	}
	return text{s: s, offs: offs}
}

// n is the number of lines.
func (t text) n() int {
	if t.lines != nil {
		return len(t.lines)
	}
	return len(t.offs)
}

// line returns line i without its terminator.
func (t text) line(i int) string {
	if t.lines != nil {
		if i < 0 || i >= len(t.lines) {
			return ""
		}
		return t.lines[i]
	}
	if i < 0 || i+1 > len(t.offs) {
		return ""
	}
	start := int(t.offs[i])
	end := len(t.s)
	if i+1 < len(t.offs) {
		end = int(t.offs[i+1])
	}
	return trimEOL(t.s[start:end])
}

// lineLen is len(line(i)) without building the string.
func (t text) lineLen(i int) int {
	if t.lines != nil {
		if i < 0 || i >= len(t.lines) {
			return 0
		}
		return len(t.lines[i])
	}
	if i < 0 || i+1 > len(t.offs) {
		return 0
	}
	start := int(t.offs[i])
	end := len(t.s)
	if i+1 < len(t.offs) {
		end = int(t.offs[i+1])
	}
	n := end - start
	if n > 0 && t.s[end-1] == '\n' {
		n--
		if n > 0 && t.s[end-2] == '\r' {
			n--
		}
	} else if n > 0 && t.s[end-1] == '\r' {
		n--
	}
	return n
}

// trimEOL removes the line terminator, matching what splitLines does.
func trimEOL(s string) string {
	s = strings.TrimSuffix(s, "\n")
	return strings.TrimSuffix(s, "\r")
}

// sameLine compares two lines, optionally ignoring whitespace runs, which is
// what the ignore-whitespace option actually means at line level.
func sameLine(a, b string, ignoreWS bool) bool {
	if !ignoreWS {
		return a == b
	}
	return normalizeWS(a) == normalizeWS(b)
}

// normalizeWS collapses whitespace runs to a single space. It returns the
// input unchanged when it is already collapsed, which is the common case and
// costs no allocation.
func normalizeWS(s string) string {
	prev := false
	for i := 0; i < len(s); i++ {
		space := isSpaceByte(s[i])
		if space && (prev || s[i] != ' ' || i == 0 || i == len(s)-1) {
			return strings.Join(strings.Fields(s), " ")
		}
		prev = space
	}
	return s
}

// bytes is the memory the indexed text keeps resident: the content, its line
// offsets, and the fallback split when the content was too large to index.
func (t text) bytes() int {
	return len(t.s) + 4*len(t.offs) + 16*len(t.lines)
}

// lineEnd is the byte offset just past line i, including its terminator.
func (t text) lineEnd(i int) int {
	if i+1 < len(t.offs) {
		return int(t.offs[i+1])
	}
	return len(t.s)
}

// lineAt is the line containing byte offset off.
func (t text) lineAt(off int) int {
	i := sort.Search(len(t.offs), func(i int) bool { return int(t.offs[i]) > off }) - 1
	if i < 0 {
		return 0
	}
	return i
}

// textIDs interns the lines of two ranges into dense integer ids.
