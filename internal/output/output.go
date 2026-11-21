// Package output renders a diff as text for the command line.
package output

import (
	"strings"

	"samouly.fun/twig/internal/diff"
)

const (
	ansiReset = "\x1b[0m"
	ansiDim   = "\x1b[2m"
	ansiGreen = "\x1b[32m"
	ansiRed   = "\x1b[31m"
	ansiCyan  = "\x1b[36m"
)

const nullPath = "/dev/null"

// Patch renders the files the way git writes a patch.
func Patch(files []*diff.File, color bool) string {
	var b strings.Builder
	for _, f := range files {
		b.WriteString(filePatch(f))
	}

	text := b.String()
	if !color {
		return text
	}
	return paint(text)
}

func filePatch(f *diff.File) string {
	if f.Kind == diff.Unmerged {
		// One column per parent has no plain patch form; print the rows anyway.
		var b strings.Builder
		b.WriteString("diff --cc " + f.Path() + "\n")
		for _, line := range f.Lines {
			b.WriteString(line.Text + "\n")
		}
		return b.String()
	}

	// Both sides are named even when one is missing; only --- and +++ say so.
	oldPath, newPath := f.Path(), f.Path()
	if f.OldPath != "" {
		oldPath = f.OldPath
	}
	if f.NewPath != "" {
		newPath = f.NewPath
	}
	from, to := "a/"+oldPath, "b/"+newPath
	oldSide, newSide := from, to
	switch f.Kind {
	case diff.Added:
		oldSide = nullPath
	case diff.Deleted:
		newSide = nullPath
	}

	var b strings.Builder
	b.WriteString("diff --git " + from + " " + to + "\n")
	if f.Binary {
		b.WriteString("Binary files " + oldSide + " and " + newSide + " differ\n")
		return b.String()
	}

	modeChanged := f.OldMode != "" && f.NewMode != "" && f.OldMode != f.NewMode
	renamed := f.Kind == diff.Renamed && f.OldPath != "" && f.NewPath != f.OldPath
	if len(f.Hunks) == 0 && !modeChanged && !renamed {
		return ""
	}
	switch {
	case f.Kind == diff.Added && f.NewMode != "":
		b.WriteString("new file mode " + f.NewMode + "\n")
	case f.Kind == diff.Deleted && f.OldMode != "":
		b.WriteString("deleted file mode " + f.OldMode + "\n")
	case modeChanged:
		b.WriteString("old mode " + f.OldMode + "\nnew mode " + f.NewMode + "\n")
	}
	if renamed {
		b.WriteString("rename from " + f.OldPath + "\nrename to " + f.NewPath + "\n")
	}
	if len(f.Hunks) == 0 {
		return b.String()
	}
	b.WriteString("--- " + oldSide + "\n+++ " + newSide + "\n")
	for i := range f.Hunks {
		b.WriteString(f.HunkPatch(i))
	}
	return b.String()
}

// StripANSI removes colour an upstream program added.
func StripANSI(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	for i := 0; i < len(src); i++ {
		if src[i] != '\x1b' || i+1 >= len(src) || src[i+1] != '[' {
			b.WriteByte(src[i])
			continue
		}
		// A CSI sequence ends at the first final byte; git only emits SGR.
		j := i + 2
		for j < len(src) && (src[j] < 'A' || src[j] > 'Z') && (src[j] < 'a' || src[j] > 'z') {
			j++
		}
		i = j
	}
	return b.String()
}

// paint colours a patch line by line, headers before signs.
func paint(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git"),
			strings.HasPrefix(line, "+++"),
			strings.HasPrefix(line, "---"),
			strings.HasPrefix(line, "index "),
			strings.HasPrefix(line, "new file mode"),
			strings.HasPrefix(line, "deleted file mode"),
			strings.HasPrefix(line, "old mode"),
			strings.HasPrefix(line, "new mode"):
			lines[i] = ansiDim + line + ansiReset
		case strings.HasPrefix(line, "@@"):
			lines[i] = ansiCyan + line + ansiReset
		case strings.HasPrefix(line, "+"):
			lines[i] = ansiGreen + line + ansiReset
		case strings.HasPrefix(line, "-"):
			lines[i] = ansiRed + line + ansiReset
		}
	}
	return strings.Join(lines, "\n")
}
