// Package stage turns a selection of changed lines into the bytes a file
// should hold. It is deliberately pure: no git, no filesystem, no UI, so the
// whole of staging is one function over three strings that a test can pin
// down.
package stage

// Side names which version of a file a line number belongs to.
type Side uint8

// The two sides of a diff.
const (
	// Pre is the file as it exists before the change: the old side.
	Pre Side = iota
	// Post is the file as it exists after the change: the new side.
	Post
)

// Line identifies one changed line. A deletion is named by its number on the
// old side, an addition by its number on the new side. Numbers come from the
// file's own content, not from a row index, so they survive a re-diff: this is
// why a selection cannot drift onto a neighbouring hunk the way git add -p's
// decisions can.
type Line struct {
	Side Side
	Num  int
}

// NewPre names the n-th line of the old side.
func NewPre(n int) Line { return Line{Side: Pre, Num: n} }

// NewPost names the n-th line of the new side.
func NewPost(n int) Line { return Line{Side: Post, Num: n} }

// Selection is a set of changed lines in one file.
type Selection struct {
	set map[Line]struct{}
}

// NewSelection returns an empty selection.
func NewSelection() *Selection {
	return &Selection{set: make(map[Line]struct{})}
}

// Add includes a line.
func (s *Selection) Add(l Line) {
	if s.set == nil {
		s.set = make(map[Line]struct{})
	}
	s.set[l] = struct{}{}
}

// Remove excludes a line.
func (s *Selection) Remove(l Line) { delete(s.set, l) }

// Has reports whether a line is selected.
func (s *Selection) Has(l Line) bool {
	_, ok := s.set[l]
	return ok
}

// Toggle flips a line and reports its new state.
func (s *Selection) Toggle(l Line) bool {
	if s.Has(l) {
		s.Remove(l)
		return false
	}
	s.Add(l)
	return true
}

// Len is the number of selected lines.
func (s *Selection) Len() int { return len(s.set) }

// Empty reports whether nothing is selected.
func (s *Selection) Empty() bool { return len(s.set) == 0 }

// Clone returns an independent copy, so a pending selection can be edited
// without disturbing the one that is already applied.
func (s *Selection) Clone() *Selection {
	c := NewSelection()
	for l := range s.set {
		c.set[l] = struct{}{}
	}
	return c
}

// Merge adds every line of other to s.
func (s *Selection) Merge(other *Selection) {
	for l := range other.set {
		s.set[l] = struct{}{}
	}
}

// Difference removes every line of other from s.
func (s *Selection) Difference(other *Selection) {
	for l := range other.set {
		delete(s.set, l)
	}
}

// Equal reports whether two selections hold the same lines.
func (s *Selection) Equal(other *Selection) bool {
	if len(s.set) != len(other.set) {
		return false
	}
	for l := range s.set {
		if _, ok := other.set[l]; !ok {
			return false
		}
	}
	return true
}

// Lines returns the selection as a slice, in no particular order.
func (s *Selection) Lines() []Line {
	out := make([]Line, 0, len(s.set))
	for l := range s.set {
		out = append(out, l)
	}
	return out
}
