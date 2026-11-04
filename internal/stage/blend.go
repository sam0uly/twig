package stage

import (
	"errors"
	"strings"

	"samouly.fun/twig/internal/diff"
)

// ErrNotResident is returned for a diff stored in pages. A paged file does not
// hold its rows, and a file that large is staged whole rather than one line at
// a time.
var ErrNotResident = errors.New("diff rows not resident")

// ErrTailOnly is returned for the one change a line selection cannot express:
// the last line is identical in both versions while its trailing newline is
// not. twig's diff model does not carry a row for a terminator, so a blend
// cannot tell its sides apart, and a partial stage would quietly write one of
// them. The caller stages such a file whole instead.
var ErrTailOnly = errors.New("only the final newline changed")

// tailUnrepresentable reports whether the two versions differ in their final
// newline while the last post line is not an addition. In that shape the
// result's last line is carried over from the preimage, so its terminator
// cannot be attributed to a selection, and a partial stage would quietly write
// one side's byte. When the last post line is an addition its terminator
// travels with the line, and the tail needs no special care.
func tailUnrepresentable(pre, post string, f *diff.File, postLines int) bool {
	if postLines == 0 {
		return false
	}
	if strings.HasSuffix(pre, "\n") == strings.HasSuffix(post, "\n") {
		return false
	}
	for _, row := range f.Lines {
		if row.Kind == diff.Addition && row.New == postLines {
			return false
		}
	}
	return true
}

// Blend returns pre with exactly the selected changed lines applied: an
// addition is inserted, a deletion is dropped, and every other line keeps the
// bytes the preimage gave it. Selecting every line reproduces post byte for
// byte; selecting none reproduces pre. Those two properties are the whole
// contract, and they are what a test asserts.
//
// f must be the diff of pre against post computed without whitespace
// tolerance, or a whitespace-only change would read as context and the blend
// would keep the old bytes.
func Blend(pre, post string, f *diff.File, sel *Selection) (string, error) {
	if f == nil {
		return "", errors.New("stage: no diff")
	}
	if f.Paged() {
		return "", ErrNotResident
	}

	preLines := splitLines(pre)
	postLines := splitLines(post)
	if sel != nil && !sel.Empty() && tailUnrepresentable(pre, post, f, len(postLines)) {
		return "", ErrTailOnly
	}
	if sel == nil || sel.Empty() {
		return pre, nil
	}

	out := make([]rawLine, 0, len(preLines)+sel.Len())
	preIdx := 0

	// emitPre copies pre lines up to and including index n.
	emitPre := func(n int) {
		for preIdx <= n && preIdx < len(preLines) {
			out = append(out, preLines[preIdx])
			preIdx++
		}
	}

	for i := 0; i < len(f.Lines); {
		row := f.Lines[i]
		switch row.Kind {
		case diff.HunkHeader, diff.NoNewline:
			i++
			continue
		case diff.Context:
			emitPre(row.Old - 1)
			i++
			continue
		}

		// A change group: everything the diff could not match, in one run of
		// deletions followed by additions. That is one edit site, so it is
		// decided as one: a replacement is accepted or refused whole, which is
		// what makes a single keystroke mean the obvious thing and what keeps
		// a half-applied pair from ever reaching the index.
		start := i
		for i < len(f.Lines) {
			k := f.Lines[i].Kind
			if k != diff.Deletion && k != diff.Addition {
				break
			}
			i++
		}
		var dels, adds []diff.Line
		for _, r := range f.Lines[start:i] {
			if r.Kind == diff.Deletion {
				dels = append(dels, r)
			} else {
				adds = append(adds, r)
			}
		}

		for j := 0; j < max(len(dels), len(adds)); j++ {
			switch {
			case j < len(dels) && j < len(adds):
				d, a := dels[j], adds[j]
				emitPre(d.Old - 2)
				// Either side of the pair names the same decision. Selecting
				// one is selecting the replacement, because there is no
				// sensible middle state to write.
				if sel.Has(NewPre(d.Old)) || sel.Has(NewPost(a.New)) {
					out = append(out, postLines[a.New-1])
				} else {
					out = append(out, preLines[d.Old-1])
				}
				preIdx = d.Old
			case j < len(dels):
				d := dels[j]
				emitPre(d.Old - 2)
				if !sel.Has(NewPre(d.Old)) {
					out = append(out, preLines[d.Old-1])
				}
				preIdx = d.Old
			default:
				a := adds[j]
				if sel.Has(NewPost(a.New)) {
					out = append(out, postLines[a.New-1])
				}
			}
		}
	}
	emitPre(len(preLines) - 1)
	return joinLines(out), nil
}

// rawLine is one line of a file with the question that a byte-level merge has
// to keep: whether it ended with a newline. That is how a file without a final
// newline survives being blended, instead of silently gaining one.
type rawLine struct {
	text string
	nl   bool
}

// splitLines splits s into lines, keeping each line's terminator. A trailing
// newline belongs to the line before it, so "a\nb\n" is two lines and "a\nb"
// is two lines of which the last has none.
func splitLines(s string) []rawLine {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	out := make([]rawLine, 0, len(parts))
	for i := 0; i < len(parts)-1; i++ {
		out = append(out, rawLine{text: parts[i], nl: true})
	}
	if last := parts[len(parts)-1]; last != "" {
		out = append(out, rawLine{text: last, nl: false})
	}
	return out
}

// joinLines rebuilds a file from lines, honouring whether each one ended with
// a newline. A line that is not last always gets one.
func joinLines(ls []rawLine) string {
	var b strings.Builder
	size := 0
	for _, l := range ls {
		size += len(l.text) + 1
	}
	b.Grow(size)

	for i, l := range ls {
		b.WriteString(l.text)
		if i < len(ls)-1 || l.nl {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// LineOf names a diff row as a selectable line: a deletion by its old number,
// an addition by its new one, and anything else as not selectable. The name
// comes from the file's content, so it survives a re-diff of the same change.
func LineOf(l diff.Line) (Line, bool) {
	switch l.Kind {
	case diff.Deletion:
		if l.Old < 1 {
			return Line{}, false
		}
		return NewPre(l.Old), true
	case diff.Addition:
		if l.New < 1 {
			return Line{}, false
		}
		return NewPost(l.New), true
	}
	return Line{}, false
}

// Pair makes a selection atomic at each edit site: a deletion and the addition
// that replaces it are one decision, so selecting either selects both. It is
// what lets "discard this line" mean "put this line back" instead of leaving
// half a replacement behind.
func Pair(f *diff.File, sel *Selection) *Selection {
	if f == nil || sel == nil || sel.Empty() {
		return sel
	}
	out := sel.Clone()
	for i := 0; i < len(f.Lines); {
		switch f.Lines[i].Kind {
		case diff.HunkHeader, diff.NoNewline, diff.Context:
			i++
			continue
		}
		start := i
		for i < len(f.Lines) {
			k := f.Lines[i].Kind
			if k != diff.Deletion && k != diff.Addition {
				break
			}
			i++
		}
		var dels, adds []diff.Line
		for _, r := range f.Lines[start:i] {
			if r.Kind == diff.Deletion {
				dels = append(dels, r)
			} else {
				adds = append(adds, r)
			}
		}
		for j := 0; j < min(len(dels), len(adds)); j++ {
			d, a := NewPre(dels[j].Old), NewPost(adds[j].New)
			if sel.Has(d) || sel.Has(a) {
				out.Add(d)
				out.Add(a)
			}
		}
	}
	return out
}

// HunkSelection returns every changed line of one hunk.
func HunkSelection(f *diff.File, hunk int) *Selection {
	sel := NewSelection()
	h, ok := f.HunkAt(hunk)
	if !ok {
		return sel
	}
	for _, l := range h.Lines {
		switch l.Kind {
		case diff.Addition:
			sel.Add(NewPost(l.New))
		case diff.Deletion:
			sel.Add(NewPre(l.Old))
		}
	}
	return sel
}

// AllSelection returns every changed line in the file.
func AllSelection(f *diff.File) *Selection {
	sel := NewSelection()
	for i := range f.Hunks {
		sel.Merge(HunkSelection(f, i))
	}
	return sel
}
