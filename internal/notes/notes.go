// Package notes holds annotations on a diff, and the store that keeps them.
package notes

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Version is the store format.
const Version = 1

// Kind is what a note sits on.
type Kind uint8

// The three things a note can sit on.
const (
	File Kind = iota
	Hunk
	Lines
)

// String implements [fmt.Stringer].
func (k Kind) String() string {
	switch k {
	case File:
		return "file"
	case Hunk:
		return "hunk"
	case Lines:
		return "lines"
	default:
		return "unknown"
	}
}

// Tag is how a note reads at a glance.
type Tag uint8

// The note tags.
const (
	Fyi Tag = iota
	Todo
	Warn
	Error

	// TagCount is how many tags there are.
	TagCount
)

// Next is the tag after this one.
func (t Tag) Next() Tag { return (t + 1) % TagCount }

// String implements [fmt.Stringer].
func (t Tag) String() string {
	switch t {
	case Todo:
		return "todo"
	case Warn:
		return "warn"
	case Error:
		return "error"
	default:
		return "fyi"
	}
}

// ParseTag reads a tag name, in any case.
func ParseTag(s string) (Tag, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "fyi", "info", "note":
		return Fyi, nil
	case "todo", "fixme":
		return Todo, nil
	case "warn", "warning", "hmm":
		return Warn, nil
	case "error", "err", "oops":
		return Error, nil
	default:
		return Fyi, fmt.Errorf("not a note tag: %q", s)
	}
}

// tagWords are the words a body may open with to set its tag.
var tagWords = map[string]Tag{
	"fyi": Fyi, "todo": Todo, "fixme": Todo,
	"warn": Warn, "warning": Warn, "hmm": Warn,
	"error": Error, "err": Error, "oops": Error,
}

// SplitTag reads a leading tag word and returns the body without it.
func SplitTag(s string) (Tag, string) {
	trimmed := strings.TrimSpace(s)
	word, rest, _ := strings.Cut(trimmed, " ")
	word = strings.ToLower(strings.TrimSuffix(word, ":"))
	if tag, ok := tagWords[word]; ok {
		return tag, strings.TrimSpace(rest)
	}
	return Fyi, trimmed
}

// Note is one annotation.
type Note struct {
	ID      string    `json:"id,omitempty"`
	Kind    Kind      `json:"kind"`
	Tag     Tag       `json:"tag"`
	Path    string    `json:"path"`
	Hunk    int       `json:"hunk,omitempty"`
	Start   int       `json:"start,omitempty"`
	End     int       `json:"end,omitempty"`
	Header  string    `json:"header,omitempty"`
	Text    []string  `json:"text,omitempty"`
	Body    string    `json:"body"`
	Created time.Time `json:"created"`
}

// Anchor is the note's position as a command line writes it.
func (n Note) Anchor() string {
	switch n.Kind {
	case Hunk:
		return n.Path + "#" + strconv.Itoa(n.Hunk)
	case Lines:
		if n.Start == n.End {
			return n.Path + ":" + strconv.Itoa(n.Start)
		}
		return n.Path + ":" + strconv.Itoa(n.Start) + "-" + strconv.Itoa(n.End)
	default:
		return n.Path
	}
}

// Validate checks the anchor against its kind.
func (n Note) Validate() error {
	if n.Path == "" {
		return errors.New("a note needs a path")
	}
	if n.Body == "" {
		return errors.New("a note needs a body")
	}
	switch n.Kind {
	case File:
		if n.Hunk != 0 || n.Start != 0 || n.End != 0 {
			return errors.New("a file note carries no hunk and no lines")
		}
	case Hunk:
		if n.Hunk < 1 {
			return errors.New("a hunk note needs a hunk number")
		}
		if n.Start != 0 || n.End != 0 {
			return errors.New("a hunk note carries no lines")
		}
	case Lines:
		if n.Start < 1 || n.End < n.Start {
			return fmt.Errorf("a line note needs a range, got %d-%d", n.Start, n.End)
		}
		if n.Hunk != 0 {
			return errors.New("a line note carries no hunk number")
		}
	default:
		return fmt.Errorf("unknown note kind %d", n.Kind)
	}
	return nil
}

// Store is one repository's notes.
type Store struct {
	Version int    `json:"v"`
	NextID  int    `json:"next_id"`
	Notes   []Note `json:"notes"`
}

// Path is where a repository keeps its notes.
func Path(gitDir string) string {
	return filepath.Join(gitDir, "twig-notes.json")
}

// Add appends a note and gives it an id.
func (s *Store) Add(note Note) Note {
	note.ID = "n" + strconv.Itoa(s.NextID)
	s.NextID++
	s.Notes = append(s.Notes, note)
	return note
}

// Remove drops the notes with the given ids and reports how many it found.
func (s *Store) Remove(ids ...string) int {
	drop := make(map[string]bool, len(ids))
	for _, id := range ids {
		drop[id] = true
	}
	kept := s.Notes[:0]
	for _, note := range s.Notes {
		if !drop[note.ID] {
			kept = append(kept, note)
		}
	}
	removed := len(s.Notes) - len(kept)
	s.Notes = kept
	return removed
}

// Clear drops the notes under a path, or all of them when it is empty.
func (s *Store) Clear(path string) int {
	if path == "" {
		n := len(s.Notes)
		s.Notes = nil
		return n
	}
	kept := s.Notes[:0]
	for _, note := range s.Notes {
		if !note.Under(path) {
			kept = append(kept, note)
		}
	}
	removed := len(s.Notes) - len(kept)
	s.Notes = kept
	return removed
}

// Under reports whether a note sits at or below path.
func (n Note) Under(path string) bool {
	if path == "" {
		return true
	}
	path = strings.TrimSuffix(path, "/")
	return n.Path == path || strings.HasPrefix(n.Path, path+"/")
}

// For returns the notes on one path, in the order they were written.
func (s Store) For(path string) []Note {
	var out []Note
	for _, note := range s.Notes {
		if note.Path == path {
			out = append(out, note)
		}
	}
	return out
}

// Has reports whether the store holds a note with this id.
func (s Store) Has(id string) bool {
	for _, note := range s.Notes {
		if note.ID == id {
			return true
		}
	}
	return false
}

// Parse reads path, path:12, path:12-14 or path#2.
func Parse(arg string) (Note, error) {
	arg = strings.TrimSuffix(arg, "/")
	if arg == "" {
		return Note{}, errors.New("an anchor needs a path")
	}

	if i := strings.LastIndex(arg, "#"); i > 0 {
		hunk, err := strconv.Atoi(arg[i+1:])
		if err != nil || hunk < 1 {
			return Note{}, fmt.Errorf("not a hunk number: %q", arg)
		}
		return Note{Kind: Hunk, Path: arg[:i], Hunk: hunk}, nil
	}

	i := strings.LastIndex(arg, ":")
	if i < 0 {
		return Note{Kind: File, Path: arg}, nil
	}
	if i == 0 {
		return Note{}, errors.New("an anchor needs a path")
	}
	path, spec := arg[:i], arg[i+1:]
	if !hasDigit(spec) {
		// The colon belongs to the path, not to a line.
		return Note{Kind: File, Path: arg}, nil
	}

	first, last, ranged := strings.Cut(spec, "-")
	start, err := strconv.Atoi(first)
	if err != nil || start < 1 {
		return Note{}, fmt.Errorf("not a line number: %q", arg)
	}
	end := start
	if ranged {
		if end, err = strconv.Atoi(last); err != nil || end < start {
			return Note{}, fmt.Errorf("not a line range: %q", arg)
		}
	}
	return Note{Kind: Lines, Path: path, Start: start, End: end}, nil
}

func hasDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}

// Resolve fills the anchored lines from the worktree.
func (n *Note) Resolve(root string) error {
	if n.Kind != Lines {
		n.Text = nil
		return nil
	}
	lines, err := fileLines(root, n.Path)
	if err != nil {
		return err
	}
	if n.End > len(lines) {
		return fmt.Errorf("no such line: %s has %d lines, not %d", n.Path, len(lines), n.End)
	}
	n.Text = append([]string(nil), lines[n.Start-1:n.End]...)
	return nil
}

// Drifted reports whether the anchor still holds.
func (n Note) Drifted(root string) bool {
	if n.Kind == Hunk {
		return false
	}
	if n.Kind == File {
		return !fileExists(root, n.Path)
	}
	if len(n.Text) == 0 {
		return false
	}
	lines, err := fileLines(root, n.Path)
	if err != nil || n.End > len(lines) {
		return true
	}
	for i, want := range n.Text {
		if lines[n.Start-1+i] != want {
			return true
		}
	}
	return false
}

// HunkDrifted reports whether the hunk is gone or its header moved.
func (n Note) HunkDrifted(header string, hunk int) bool {
	return n.Kind == Hunk && (n.Hunk != hunk || (n.Header != "" && n.Header != header))
}
