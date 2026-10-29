package diff

import "strconv"

// Aggregate combines several file diffs into one synthetic File so a commit
// view can render every changed file in a single scrollable document. File
// separator lines are rendered as HunkHeader lines, so they pick up the
// spanning band style and register as navigation targets.
func Aggregate(files []*File) *File {
	combined := &File{Kind: Modified}
	for _, f := range files {
		combined.Append(f)
	}
	return combined
}

// Append adds one file's lines and hunks to the end of f. Existing positions
// are untouched, so a document can grow block by block while it is on screen:
// only rows below the already rendered ones appear. A paged source is
// materialised on the way in: the aggregate is one document and cannot page
// its parts independently.
func (f *File) Append(g *File) {
	offset := len(f.Lines)

	sep := "── " + g.Path()
	if !g.Binary {
		sep += "  +" + strconv.Itoa(g.Additions) + " -" + strconv.Itoa(g.Deletions)
	} else {
		sep += "  binary"
	}
	f.Lines = append(f.Lines, Line{Kind: HunkHeader, Text: sep})
	f.Lines = append(f.Lines, g.LinesRange(0, g.NLines())...)

	for i := range g.NHunks() {
		h, ok := g.HunkAt(i)
		if !ok {
			continue
		}
		h.LineIndex += offset + 1
		f.Hunks = append(f.Hunks, h)
	}

	f.Additions += g.Additions
	f.Deletions += g.Deletions
}
