// Package diff parses unified diff output produced by git into a structured
// model that can be rendered by the UI layer.
package diff

import (
	"strconv"
	"strings"
)

// ChangeKind describes how a file changed relative to its previous state.
type ChangeKind byte

// Possible file change kinds.
const (
	Modified ChangeKind = 'M'
	Added    ChangeKind = 'A'
	Deleted  ChangeKind = 'D'
	Renamed  ChangeKind = 'R'
	Copied   ChangeKind = 'C'
)

// String implements [fmt.Stringer].
func (c ChangeKind) String() string {
	switch c {
	case Added:
		return "added"
	case Deleted:
		return "deleted"
	case Renamed:
		return "renamed"
	case Copied:
		return "copied"
	default:
		return "modified"
	}
}

// LineKind describes the kind of a rendered diff line.
type LineKind uint8

// Possible line kinds.
const (
	// Context is an unchanged line.
	Context LineKind = iota
	// Addition is a line present only in the new version.
	Addition
	// Deletion is a line present only in the old version.
	Deletion
	// HunkHeader is a "@@ -a,b +c,d @@ ctx" line.
	HunkHeader
	// NoNewline is a "\\ No newline at end of file" line.
	NoNewline
)

// Line is a single rendered diff line.
type Line struct {
	Kind LineKind
	// Text is the rendered text including the +/-/space sign prefix, when
	// applicable.
	Text string
	// Old and New are the line numbers in the old and new files. 0 means the
	// number does not apply (added lines have no old number, and so on).
	Old int
	New int

	// words holds changed-word spans (see worddiff.go), as byte offsets into
	// Text.
	words []Span
}

// Hunk is a contiguous group of changes in a file.
type Hunk struct {
	// Header is the raw hunk header, e.g. "@@ -12,7 +12,9 @@ someFunc".
	Header string
	// Lines are the body lines of the hunk, without the header.
	Lines []Line
	// LineIndex is the index of the hunk header in the file's rendered lines.
	LineIndex int
}

// File holds the parsed diff of a single file.
type File struct {
	OldPath string
	NewPath string
	Kind    ChangeKind
	Binary  bool
	// OldMode and NewMode are the octal file modes when they changed.
	OldMode string
	NewMode string
	// Lines is the flat list of rendered lines (hunk headers included). A
	// paged file leaves it empty and rebuilds pages on demand from page.
	Lines []Line
	// Hunks are the parsed hunks, in order. A paged file stores only their
	// headers and positions; HunkAt materialises a body.
	Hunks []Hunk

	Additions int
	Deletions int

	// Segments are the files an aggregate was built from, in row order, each
	// with the row its band starts on. Rows from one segment to the next belong
	// to that segment's file, which is how a commit view knows which language a
	// row is written in. A single file's diff has none.
	Segments []Segment

	// page holds a diff too large to keep as rendered lines.
	page *plan
}

// Segment is one file's run of rows inside an aggregate document.
type Segment struct {
	// Row is the first row of the segment, its file's separator band.
	Row int
	// Path is the file those rows were written from, which names the language
	// they are read in. The rows themselves live in the aggregate, so nothing
	// is held twice.
	Path string
}

// Unavailable is a one-line stand-in for a file whose diff could not be built.
// It keeps the caller's one-to-one mapping between a listing row and a document
// band, and it tells the reader why instead of showing an empty file.
func Unavailable(path, reason string) *File {
	text := "diff unavailable"
	if reason != "" {
		text += ": " + reason
	}
	return &File{
		OldPath: path,
		NewPath: path,
		Kind:    Modified,
		Lines:   []Line{{Kind: NoNewline, Text: text}},
	}
}

// Path returns the display path of the file.
func (f *File) Path() string {
	switch {
	case f.NewPath != "" && f.NewPath != "/dev/null":
		return f.NewPath
	case f.OldPath != "" && f.OldPath != "/dev/null":
		return f.OldPath
	default:
		return f.NewPath
	}
}

// Patch renders the file diff back as a unified diff patch.
func (f *File) Patch() string {
	var b strings.Builder
	oldPath, newPath := f.OldPath, f.NewPath
	if oldPath == "" {
		oldPath = f.Path()
	}
	if newPath == "" {
		newPath = f.Path()
	}
	b.WriteString("diff --git a/")
	b.WriteString(oldPath)
	b.WriteString(" b/")
	b.WriteString(newPath)
	b.WriteString("\n")
	if f.Kind != Modified {
		b.WriteString("--- ")
		b.WriteString(oldPath)
		b.WriteString("\n")
		b.WriteString("+++ ")
		b.WriteString(newPath)
		b.WriteString("\n")
	}
	for i := range f.Hunks {
		b.WriteString(f.HunkPatch(i))
	}
	return b.String()
}

// NLines is the number of rendered lines in the file.
func (f *File) NLines() int {
	if f.page != nil {
		return f.page.rows()
	}
	return len(f.Lines)
}

// Paged reports whether the lines are generated on demand instead of stored.
func (f *File) Paged() bool { return f.page != nil }

// LineAt returns rendered line i.
func (f *File) LineAt(i int) (Line, bool) {
	if i < 0 || i >= f.NLines() {
		return Line{}, false
	}
	if f.page != nil {
		return f.page.lines(i, i+1)[0], true
	}
	return f.Lines[i], true
}

// LineText is the rendered text of line i, without building the line.
func (f *File) LineText(i int) string {
	if f.page != nil {
		return f.page.textAt(i)
	}
	if i < 0 || i >= len(f.Lines) {
		return ""
	}
	return f.Lines[i].Text
}

// LineKindAt reports the kind of line i, without building it.
func (f *File) LineKindAt(i int) LineKind {
	if f.page != nil {
		return f.page.kindAt(i)
	}
	if i < 0 || i >= len(f.Lines) {
		return Context
	}
	return f.Lines[i].Kind
}

// ChangeAt reports whether line i is part of a change run, which is where a
// renderer may not split the document: split mode pairs deletions with
// additions, so half a run renders differently from the whole.
func (f *File) ChangeAt(i int) bool {
	switch f.LineKindAt(i) {
	case Addition, Deletion:
		return true
	default:
		return false
	}
}

// LinesRange returns rendered lines [from, to). A resident file returns a
// sub-slice, a paged one rebuilds the range and hands back a fresh slice.
func (f *File) LinesRange(from, to int) []Line {
	from = max(0, from)
	to = min(to, f.NLines())
	if from >= to {
		return nil
	}
	if f.page != nil {
		return f.page.lines(from, to)
	}
	return f.Lines[from:to]
}

// MatchRows finds term in rendered rows [from, to), reporting each match's row
// and its columns inside that row's rendered text. A paged diff answers by
// scanning its sources, which is much cheaper than formatting every line; the
// bool is false when this diff cannot answer that way (it is resident, or the
// term needs the rendered text), leaving the caller to scan lines itself.
func (f *File) MatchRows(term string, from, to int) ([]MatchRow, bool) {
	if f.page == nil {
		return nil, false
	}
	return f.page.matches(term, from, to)
}

// ScanLines calls fn for each rendered line in [from, to) with its kind and
// text width. It is the layout path: a paged file lays millions of rows out
// without allocating a single Line.
func (f *File) ScanLines(from, to int, fn func(kind LineKind, width int)) {
	if f.page != nil {
		f.page.scan(from, to, fn)
		return
	}
	from = max(0, from)
	to = min(to, len(f.Lines))
	for _, l := range f.Lines[from:to] {
		fn(l.Kind, len(l.Text))
	}
}

// NHunks is the number of hunks in the file.
func (f *File) NHunks() int { return len(f.Hunks) }

// HunkAt returns hunk i with its body lines.
func (f *File) HunkAt(i int) (Hunk, bool) {
	if i < 0 || i >= len(f.Hunks) {
		return Hunk{}, false
	}
	h := f.Hunks[i]
	if h.Lines == nil {
		if f.page != nil {
			h.Lines = f.page.hunkLines(i)
		} else {
			end := len(f.Lines)
			if i+1 < len(f.Hunks) {
				end = f.Hunks[i+1].LineIndex
			}
			h.Lines = f.Lines[min(h.LineIndex+1, end):end]
		}
	}
	return h, true
}

// HunkPatch renders hunk i as patch text, header included.
func (f *File) HunkPatch(i int) string {
	h, ok := f.HunkAt(i)
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString(h.Header)
	b.WriteString("\n")
	for _, l := range h.Lines {
		b.WriteString(l.Text)
		b.WriteString("\n")
	}
	return b.String()
}

// Struct sizes the size estimate charges per row and per hunk. They are spelled
// out rather than measured so the estimate is stable across platforms and
// toolchains; being a little off only shifts when a cache evicts.
const (
	lineBytes = 40
	hunkBytes = 48
)

// Bytes estimates the memory this diff keeps resident: its rows, its hunks,
// and, for a paged diff, the packed descriptors and the sources they rebuild
// from. Caches bound themselves by this so a handful of huge files cannot pin
// the process.
func (f *File) Bytes() int {
	if f == nil {
		return 0
	}
	n := len(f.Lines)*lineBytes + len(f.Hunks)*hunkBytes
	for _, l := range f.Lines {
		n += len(l.Text) + len(l.words)*8
	}
	if f.page != nil {
		n += f.page.bytes()
	}
	return n
}

// Slim returns a copy of the file without its content: the identity and the
// counts a listing row needs, and nothing to paint. A sidebar of a cached
// commit only needs this much, since the commit's document already holds the
// rows.
func (f *File) Slim() *File {
	if f == nil {
		return nil
	}
	return &File{
		OldPath:   f.OldPath,
		NewPath:   f.NewPath,
		Kind:      f.Kind,
		Binary:    f.Binary,
		OldMode:   f.OldMode,
		NewMode:   f.NewMode,
		Additions: f.Additions,
		Deletions: f.Deletions,
	}
}

// Diff is a parsed diff of one or more files.
type Diff struct {
	Files []*File
	// Additions and Deletions are totals across all files.
	Additions int
	Deletions int
}

// FileByPath returns the file with the given path, or nil.
func (d *Diff) FileByPath(path string) *File {
	for _, f := range d.Files {
		if f.Path() == path {
			return f
		}
	}
	return nil
}

// Parse parses unified diff output. It is tolerant of unknown lines, which
// are skipped, so that partial or unusual git output still yields a usable
// result.
func Parse(src string) *Diff {
	d := &Diff{}
	var cur *File
	sc := &scanner{lines: strings.Split(src, "\n")}

	for sc.next() {
		line := strings.TrimSuffix(sc.line(), "\r")

		switch {
		case strings.HasPrefix(line, "diff --git "):
			cur = &File{Kind: Modified}
			cur.OldPath, cur.NewPath = parseDiffGitLine(line)
			d.Files = append(d.Files, cur)

		case cur == nil:
			// Lines before the first file header are ignored.
			continue

		case strings.HasPrefix(line, "old mode "):
			cur.OldMode = strings.TrimPrefix(line, "old mode ")

		case strings.HasPrefix(line, "new mode "):
			cur.NewMode = strings.TrimPrefix(line, "new mode ")

		case strings.HasPrefix(line, "new file mode "):
			cur.Kind = Added

		case strings.HasPrefix(line, "deleted file mode "):
			cur.Kind = Deleted

		case strings.HasPrefix(line, "rename from "):
			cur.Kind = Renamed
			cur.OldPath = strings.TrimPrefix(line, "rename from ")

		case strings.HasPrefix(line, "rename to "):
			cur.NewPath = strings.TrimPrefix(line, "rename to ")

		case strings.HasPrefix(line, "copy from "):
			cur.Kind = Copied
			cur.OldPath = strings.TrimPrefix(line, "copy from ")

		case strings.HasPrefix(line, "copy to "):
			cur.NewPath = strings.TrimPrefix(line, "copy to ")

		case strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "GIT binary patch"):
			cur.Binary = true
			cur.Lines = append(cur.Lines, Line{
				Kind: NoNewline,
				Text: "Binary file differs",
			})

		case strings.HasPrefix(line, "--- "):
			if p := parseFilePath(strings.TrimPrefix(line, "--- ")); p != "" {
				cur.OldPath = p
			}

		case strings.HasPrefix(line, "+++ "):
			if p := parseFilePath(strings.TrimPrefix(line, "+++ ")); p != "" {
				cur.NewPath = p
			}

		case strings.HasPrefix(line, "@@ "):
			h := &Hunk{Header: line, LineIndex: len(cur.Lines)}
			cur.Lines = append(cur.Lines, Line{Kind: HunkHeader, Text: line})
			old, new := parseHunkHeader(line)
			parseHunkBody(sc, cur, h, old, new)
			cur.Hunks = append(cur.Hunks, *h)

		case strings.HasPrefix(line, "\\"):
			// "\ No newline at end of file"; attach to the flat lines and the
			// trailing hunk, if any.
			l := Line{Kind: NoNewline, Text: line}
			cur.Lines = append(cur.Lines, l)
			if n := len(cur.Hunks); n > 0 {
				cur.Hunks[n-1].Lines = append(cur.Hunks[n-1].Lines, l)
			}
		}
	}

	for _, f := range d.Files {
		if !f.Binary {
			for _, h := range f.Hunks {
				for _, l := range h.Lines {
					switch l.Kind {
					case Addition:
						f.Additions++
					case Deletion:
						f.Deletions++
					}
				}
			}
		}
		d.Additions += f.Additions
		d.Deletions += f.Deletions
		annotateWordDiff(f)
	}

	return d
}

// parseHunkBody consumes the body of a hunk from the scanner, tracking old
// and new line numbers. It stops at the first line that is not part of the
// hunk, leaving the scanner positioned so that line is seen by the caller.
func parseHunkBody(sc *scanner, f *File, h *Hunk, old, new [2]int) {
	oldLeft, newLeft := old[1], new[1]
	oldNum, newNum := old[0], new[0]

	for (oldLeft > 0 || newLeft > 0) && sc.next() {
		line := strings.TrimSuffix(sc.line(), "\r")
		if line == "" {
			// A trailing empty line: git separates hunks/files with an empty
			// line, which is not part of the hunk.
			break
		}
		switch line[0] {
		case ' ':
			if oldLeft == 0 || newLeft == 0 {
				sc.pushBack()
				return
			}
			h.Lines = append(h.Lines, Line{Kind: Context, Text: line, Old: oldNum, New: newNum})
			oldNum, newNum, oldLeft, newLeft = oldNum+1, newNum+1, oldLeft-1, newLeft-1
		case '+':
			if newLeft == 0 {
				sc.pushBack()
				return
			}
			h.Lines = append(h.Lines, Line{Kind: Addition, Text: line, New: newNum})
			newNum, newLeft = newNum+1, newLeft-1
		case '-':
			if oldLeft == 0 {
				sc.pushBack()
				return
			}
			h.Lines = append(h.Lines, Line{Kind: Deletion, Text: line, Old: oldNum})
			oldNum, oldLeft = oldNum+1, oldLeft-1
		default:
			// "\ No newline at end of file" belongs to the hunk body.
			if strings.HasPrefix(line, "\\") {
				l := Line{Kind: NoNewline, Text: line}
				h.Lines = append(h.Lines, l)
				f.Lines = append(f.Lines, l)
				continue
			}
			// Not a hunk body line (e.g. another header); leave it for the
			// caller.
			sc.pushBack()
			return
		}
		f.Lines = append(f.Lines, h.Lines[len(h.Lines)-1])
	}
}

// parseDiffGitLine parses the paths out of a "diff --git a/x b/y" line.
func parseDiffGitLine(line string) (old, new string) {
	rest := strings.TrimPrefix(line, "diff --git ")
	// Paths may be quoted; try the quoted form first.
	if i := strings.LastIndex(rest, " b/"); i > 0 {
		o := strings.TrimPrefix(rest[:i], "a/")
		n := strings.TrimPrefix(rest[i+1:], "b/")
		return o, n
	}
	return rest, rest
}

// parseFilePath parses the path out of a "--- a/x" / "+++ b/y" line.
func parseFilePath(s string) string {
	if s == "/dev/null" {
		return s
	}
	if strings.HasPrefix(s, "a/") || strings.HasPrefix(s, "b/") {
		return s[2:]
	}
	// Strip trailing timestamp, if any.
	if i := strings.Index(s, "\t"); i >= 0 {
		s = s[:i]
	}
	return s
}

// parseHunkHeader parses the start line and count of the old and new sides
// of a hunk header.
func parseHunkHeader(line string) (old, new [2]int) {
	parse := func(s string) [2]int {
		var out [2]int
		s = strings.TrimSpace(strings.TrimLeft(s, "-+"))
		parts := strings.SplitN(s, ",", 2)
		out[0], _ = strconv.Atoi(strings.TrimSpace(parts[0]))
		if len(parts) == 2 {
			out[1], _ = strconv.Atoi(strings.TrimSpace(parts[1]))
		} else {
			// "single line" hunk form: -N +N means count 1.
			out[1] = 1
		}
		return out
	}

	// @@ -l,c +l,c @@ ...
	rest := strings.TrimPrefix(line, "@@ ")
	if i := strings.Index(rest, "+"); i > 0 {
		old = parse(rest[:i])
		rest = rest[i:]
		if j := strings.Index(rest, " @@"); j > 0 {
			new = parse(rest[:j])
		}
	}
	return old, new
}

// scanner is a line scanner with one line of push-back.
type scanner struct {
	lines []string
	pos   int
}

func (s *scanner) next() bool {
	if s.pos >= len(s.lines) {
		return false
	}
	line := s.lines[s.pos]
	s.pos++
	// The final element after a trailing newline is empty; stop there.
	if s.pos == len(s.lines) && line == "" {
		return false
	}
	return true
}

func (s *scanner) line() string {
	return s.lines[s.pos-1]
}

func (s *scanner) pushBack() {
	s.pos--
}
