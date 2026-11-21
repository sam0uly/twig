package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"samouly.fun/twig/internal/config"
	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/notes"
)

// newNotesCommand reads and writes notes. Use --json from a tool.
func newNotesCommand(o *options) *cobra.Command {
	var (
		asJSON bool
		under  string
		brief  bool
	)
	cmd := &cobra.Command{
		Use:   "notes",
		Short: "Notes on files, hunks and lines",
		Long: `Read the notes left on this repository's diff.

A note sits on a file, a hunk or a range of lines, and lives in
.git/twig-notes.json. --json prints one object, with the anchored text and a
drifted flag, for a tool to consume.`,
		Example: `
  twig notes
  twig notes --json
  twig notes add internal/ui/tui.go:120 -m "this hides the cursor"
  twig notes add internal/ui/tui.go#3 -m "the whole hunk"
  twig notes rm n1 n2
`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return listNotes(c, o, asJSON, under, brief)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print one JSON object for a tool")
	cmd.Flags().StringVar(&under, "path", "", "only notes at or below this path")
	cmd.Flags().BoolVar(&brief, "brief", false, "one line per note, no code context")

	cmd.AddCommand(
		newNotesAddCommand(o),
		newNotesRemoveCommand(),
		newNotesClearCommand(),
	)
	return cmd
}

// openNotes loads the store, its path and the root.
func openNotes() (notes.Store, string, string, error) {
	gitDir, err := git.GitDir()
	if err != nil {
		return notes.Store{}, "", "", notesError(err)
	}
	root, err := git.RepoRoot()
	if err != nil {
		return notes.Store{}, "", "", notesError(err)
	}
	path := notes.Path(gitDir)
	store, err := notes.Load(path)
	if err != nil {
		return notes.Store{}, path, root, err
	}
	return store, path, root, nil
}

// notesError says what to do about a git failure.
func notesError(err error) error {
	if errors.Is(err, git.ErrNotARepository) {
		return errors.New("notes need a git repository")
	}
	return err
}

// noteView is one note as a tool reads it.
type noteView struct {
	Created time.Time `json:"created"`
	Kind    string    `json:"kind"`
	Tag     string    `json:"tag"`
	Anchor  string    `json:"anchor"`
	Path    string    `json:"path"`
	ID      string    `json:"id"`
	Body    string    `json:"body"`
	Header  string    `json:"header,omitempty"`
	Text    []string  `json:"text,omitempty"`
	Context []string  `json:"context,omitempty"`
	Start   int       `json:"start,omitempty"`
	End     int       `json:"end,omitempty"`
	Hunk    int       `json:"hunk,omitempty"`
	Drifted bool      `json:"drifted"`
}

func listNotes(c *cobra.Command, o *options, asJSON bool, under string, brief bool) error {
	store, _, root, err := openNotes()
	if err != nil {
		return err
	}
	cfg, _, err := o.loadConfig(c)
	if err != nil {
		return err
	}

	list := make([]notes.Note, 0, len(store.Notes))
	for _, note := range store.Notes {
		if note.Under(under) {
			list = append(list, note)
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Path != list[j].Path {
			return list[i].Path < list[j].Path
		}
		if list[i].Hunk != list[j].Hunk {
			return list[i].Hunk < list[j].Hunk
		}
		if list[i].Start != list[j].Start {
			return list[i].Start < list[j].Start
		}
		return list[i].ID < list[j].ID
	})

	diffs := map[string]*diff.File{}
	drifted := func(note notes.Note) bool {
		if note.Kind == notes.Hunk {
			return hunkDrifted(note, diffs, cfg)
		}
		return note.Drifted(root)
	}
	context := func(note notes.Note) []string {
		return noteContext(note, noteDiff(note.Path, diffs, cfg))
	}

	if !asJSON {
		var b strings.Builder
		// Notes on one anchor are listed together, above the code they share.
		for i := 0; i < len(list); {
			anchor := list[i].Anchor()
			first := i
			for i < len(list) && list[i].Anchor() == anchor {
				note := list[i]
				fmt.Fprintf(&b, "%-5s %s  %s", strings.ToUpper(note.Tag.String()), note.ID, anchor)
				if drifted(note) {
					b.WriteString("  (drifted)")
				}
				b.WriteString("  ")
				b.WriteString(strings.ReplaceAll(note.Body, "\n", "\n      "))
				b.WriteString("\n")
				i++
			}
			if brief {
				continue
			}
			for _, line := range context(list[first]) {
				b.WriteString("      ")
				b.WriteString(line)
				b.WriteString("\n")
			}
		}
		_, err := io.WriteString(c.OutOrStdout(), b.String())
		return err
	}

	views := make([]noteView, len(list))
	for i, note := range list {
		views[i] = noteView{
			ID:      note.ID,
			Kind:    note.Kind.String(),
			Tag:     note.Tag.String(),
			Anchor:  note.Anchor(),
			Path:    note.Path,
			Hunk:    note.Hunk,
			Start:   note.Start,
			End:     note.End,
			Header:  note.Header,
			Drifted: drifted(note),
			Text:    note.Text,
			Context: context(note),
			Body:    note.Body,
			Created: note.Created,
		}
	}
	enc := json.NewEncoder(c.OutOrStdout())
	enc.SetEscapeHTML(false)
	return enc.Encode(struct {
		Notes   []noteView `json:"notes"`
		Version int        `json:"v"`
	}{Version: notes.Version, Notes: views})
}

// hunkDrifted reports whether the hunk is gone or its header moved.
func hunkDrifted(note notes.Note, cache map[string]*diff.File, cfg config.Config) bool {
	file := noteDiff(note.Path, cache, cfg)
	if file == nil {
		return true
	}
	hunk, ok := file.HunkAt(note.Hunk - 1)
	if !ok {
		return true
	}
	return note.Header != "" && note.Header != hunk.Header
}

// noteDiff is the worktree diff of a path, or its staged one.
func noteDiff(path string, cache map[string]*diff.File, cfg config.Config) *diff.File {
	if file, seen := cache[path]; seen {
		return file
	}
	file := buildNoteDiff(path, cfg)
	cache[path] = file
	return file
}

func buildNoteDiff(path string, cfg config.Config) *diff.File {
	opts := git.Options{Context: cfg.Context, IgnoreWhitespace: cfg.IgnoreWhitespace}
	if file, err := git.FileDiff(path, false, opts); err == nil {
		return file
	}
	file, err := git.FileDiff(path, true, opts)
	if err != nil {
		return nil
	}
	return file
}

// noteContext is the code a note sits on: the hunk header and its neighbours.
func noteContext(note notes.Note, file *diff.File) []string {
	if file == nil || len(file.Lines) == 0 {
		return nil
	}
	switch note.Kind {
	case notes.Hunk:
		if hunk, ok := file.HunkAt(note.Hunk - 1); ok {
			return []string{hunk.Header}
		}
		return nil
	case notes.Lines:
		row := anchorRow(file, note)
		if row < 0 {
			return nil
		}
		start, end := max(0, row-2), min(len(file.Lines), row+3)
		for i := row; i >= 0; i-- {
			if file.Lines[i].Kind == diff.HunkHeader {
				start = min(start, i)
				break
			}
		}
		out := make([]string, 0, end-start)
		for _, line := range file.Lines[start:end] {
			out = append(out, line.Text)
		}
		return out
	default:
		return nil
	}
}

// anchorRow is the row a line note is about.
func anchorRow(file *diff.File, note notes.Note) int {
	want := ""
	if len(note.Text) > 0 {
		want = strings.TrimSuffix(note.Text[len(note.Text)-1], "\r")
	}
	fallback := -1
	for i, line := range file.Lines {
		if line.Kind != diff.Context && line.Kind != diff.Addition {
			continue
		}
		if line.New != note.End {
			continue
		}
		fallback = i
		if want == "" || line.Content() == want {
			return i
		}
	}
	if want != "" {
		for i, line := range file.Lines {
			if line.Kind != diff.Context && line.Kind != diff.Addition {
				continue
			}
			if line.Content() == want {
				return i
			}
		}
	}
	return fallback
}

func newNotesAddCommand(o *options) *cobra.Command {
	var (
		message string
		tag     string
	)
	cmd := &cobra.Command{
		Use:   "add ANCHOR",
		Short: "Note a file, a hunk or a range of lines",
		Long: `Note a file, a hunk or a range of lines.

ANCHOR is a path, path#HUNK (1-based, in that file's diff), path:LINE, or
path:START-END. Without --message the body is read from standard input.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			note, err := notes.Parse(args[0])
			if err != nil {
				return err
			}
			body, err := noteBody(c, message)
			if err != nil {
				return err
			}
			// A tag word in front of the body sets the tag, unless --tag says so.
			note.Body = body
			if c.Flags().Changed("tag") {
				if note.Tag, err = notes.ParseTag(tag); err != nil {
					return err
				}
			} else {
				note.Tag, note.Body = notes.SplitTag(body)
			}
			if note.Body == "" {
				return errors.New("a note needs a body")
			}

			store, storePath, root, err := openNotes()
			if err != nil {
				return err
			}
			if note.Path, err = repoPath(root, note.Path); err != nil {
				return err
			}
			cfg, _, err := o.loadConfig(c)
			if err != nil {
				return err
			}
			switch note.Kind {
			case notes.Hunk:
				if err := resolveHunk(&note, cfg); err != nil {
					return err
				}
			case notes.Lines:
				if err := note.Resolve(root); err != nil {
					return err
				}
			case notes.File:
				if _, err := os.Stat(git.WorktreePath(note.Path)); err != nil {
					return fmt.Errorf("not in the work tree: %s", note.Path)
				}
			}
			if err := note.Validate(); err != nil {
				return err
			}

			note.Created = time.Now().UTC()
			added := store.Add(note)
			if err := store.Save(storePath); err != nil {
				return err
			}
			_, err = fmt.Fprintln(c.OutOrStdout(), added.ID)
			return err
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "the note body")
	cmd.Flags().StringVar(&tag, "tag", "fyi", "fyi, todo, warn or error")
	return cmd
}

// resolveHunk fills a hunk note's header from the current diff.
func resolveHunk(note *notes.Note, cfg config.Config) error {
	file := buildNoteDiff(note.Path, cfg)
	if file == nil {
		return fmt.Errorf("no diff to note: %s", note.Path)
	}
	hunk, ok := file.HunkAt(note.Hunk - 1)
	if !ok {
		return fmt.Errorf("no such hunk: %s has %d", note.Path, file.NHunks())
	}
	note.Header = hunk.Header
	return nil
}

// repoPath makes an anchor relative to the work tree root.
func repoPath(root, arg string) (string, error) {
	path := arg
	if !filepath.IsAbs(path) {
		// The session cwd is symlink-resolved, so it matches the root git
		// reports; os.Getwd can answer in the link's spelling.
		cwd, err := git.Cwd()
		if err != nil {
			return "", err
		}
		path = filepath.Join(cwd, path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("not inside the repository: %s", arg)
	}
	return rel, nil
}

// noteBody is --message, or what was piped in.
func noteBody(c *cobra.Command, message string) (string, error) {
	if message != "" {
		return message, nil
	}
	in := c.InOrStdin()
	if in == os.Stdin && stdinIsTerminal() {
		return "", errors.New("a note needs a --message or a body on standard input")
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return "", err
	}
	body := strings.TrimSpace(string(data))
	if body == "" {
		return "", errors.New("a note needs a --message or a body on standard input")
	}
	return body, nil
}

// stdinIsTerminal reports whether standard input is a terminal.
func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func newNotesRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rm ID...",
		Short: "Remove notes by id",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			store, storePath, _, err := openNotes()
			if err != nil {
				return err
			}
			var missing []string
			for _, id := range args {
				if !store.Has(id) {
					missing = append(missing, id)
				}
			}
			if removed := store.Remove(args...); removed > 0 {
				if err := store.Save(storePath); err != nil {
					return err
				}
			}
			if len(missing) > 0 {
				return fmt.Errorf("no such note: %s", strings.Join(missing, " "))
			}
			return nil
		},
	}
}

func newNotesClearCommand() *cobra.Command {
	var under string
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Remove every note, or every note under a path",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			store, storePath, _, err := openNotes()
			if err != nil {
				return err
			}
			if store.Clear(under) == 0 {
				return nil
			}
			return store.Save(storePath)
		},
	}
	cmd.Flags().StringVar(&under, "path", "", "only notes at or below this path")
	return cmd
}
