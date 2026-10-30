package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/git"
)

// longDiff builds a diff long enough to scroll: every line differs, so the
// whole file is one hunk rather than a few context lines.
func longDiff(marker string) *diff.File {
	var old, cur strings.Builder
	for i := range 200 {
		fmt.Fprintf(&old, "old %d\n", i)
		fmt.Fprintf(&cur, "%s %d\n", marker, i)
	}
	return diff.Generate("a.go", diff.Modified, old.String(), cur.String(),
		diff.GenerateOptions{Context: 3})
}

// TestRefreshKeepsTheReaderInPlace covers what a live refresh must not do:
// move a reader who is halfway down a long diff. The diff cache hands back the
// same object when the file did not change, so the pane is left alone, and a
// changed diff is redrawn at the same line.
func TestRefreshKeepsTheReaderInPlace(t *testing.T) {
	m := testModel(120, 30)
	m.files.SetFiles(nil, []git.FileStatus{{Path: "a.go", Letter: "M"}})
	m.files.SelectPath("a.go", false)

	first := longDiff("new")
	m.view.SetFile(first)
	m.lastLoaded.path, m.lastLoaded.staged = "a.go", false
	m.view.JumpToLine(60)
	if got := m.view.Anchor(); got != 60 {
		t.Fatalf("anchor = %d, want 60", got)
	}

	// A refresh that finds the same diff must not touch the pane at all.
	next, _ := m.Update(fileLoadedMsg{file: first, path: "a.go", seq: m.seq})
	if got := next.(model).view.Anchor(); got != 60 {
		t.Fatalf("unchanged refresh moved the reader to %d", got)
	}

	// A diff that did change is redrawn, still where the reader was.
	second := longDiff("brand new")
	next, _ = m.Update(fileLoadedMsg{file: second, path: "a.go", seq: m.seq})
	nm := next.(model)
	if nm.view.File() != second {
		t.Fatal("the new diff was not displayed")
	}
	if got := nm.view.Anchor(); got != 60 {
		t.Fatalf("refresh moved the reader to %d, want 60", got)
	}
}

// TestStatusLoadStartsWatcher covers the wiring: the first status load brings
// a watcher up, and one reported change queues exactly one status run.
func TestStatusLoadStartsWatcher(t *testing.T) {
	m := testModel(120, 30)

	cmd := m.watchCommands(nil, nil)
	if cmd == nil {
		t.Fatal("the first status load should start a watcher")
	}
	started, ok := cmd().(watchStartedMsg)
	if !ok {
		t.Fatalf("watch command produced %T", cmd())
	}
	if started.err != nil {
		t.Skipf("no watcher on this platform: %v", started.err)
	}
	t.Cleanup(func() { _ = started.w.Close() })

	next, _ := m.Update(started)
	watching := next.(model)
	if watching.watcher == nil {
		t.Fatal("watcher not installed")
	}

	next, cmd = watching.Update(watchChangedMsg{})
	if cmd == nil {
		t.Fatal("a change should schedule a status run")
	}
	changed := next.(model)
	if !changed.statusPending {
		t.Fatal("status run not marked in flight")
	}
	if !changed.watchStarting {
		// watchStarting is only set while a creation command is out; it must
		// not have been left set after the watcher arrived.
		return
	}
	t.Fatal("watchStarting was not cleared when the watcher arrived")
}

// TestAllChangesRefreshRestoresTheAnchor covers the virtual document, where a
// refresh rebuilds every block: the reader still has to land on the line they
// were reading rather than on the file the sidebar selects.
func TestAllChangesRefreshRestoresTheAnchor(t *testing.T) {
	m := testModel(120, 30)
	m.files.SetFiles(nil, []git.FileStatus{{Path: "a.go", Letter: "M"}})
	m.files.SelectPath("a.go", false)

	first := longDiff("new")
	m.view.SetDocument(func(int) *diff.File { return first })
	m.view.AppendBlocks([]*diff.File{first})
	m.view.JumpToLine(60)
	if got := m.view.Anchor(); got != 60 {
		t.Fatalf("anchor before refresh = %d, want 60", got)
	}

	m.allChanges = true
	m.allAnchor = m.view.Anchor()
	next, _ := m.startAllChanges()
	nm := next.(model)

	second := longDiff("brand new")
	next, _ = nm.Update(allChunkMsg{
		entries: nm.files.OrderedEntries(),
		files:   []*diff.File{second},
		gen:     nm.allGen,
		done:    true,
	})
	if got := next.(model).view.Anchor(); got != 60 {
		t.Fatalf("anchor after rebuilding the document = %d, want 60", got)
	}
}

// TestPollInterval covers how often the backstop runs: often when polling is
// the only signal, rarely when events arrive, and never faster than a status
// run is cheap.
func TestPollInterval(t *testing.T) {
	tests := []struct {
		name    string
		watched bool
		cost    time.Duration
		want    time.Duration
	}{
		{"cheap and watched", true, 45 * time.Millisecond, pollFloorWatched},
		{"cheap and alone", false, 45 * time.Millisecond, pollFloorAlone},
		{"slow and watched", true, 2 * time.Second, 40 * time.Second},
		{"slow and alone", false, 2 * time.Second, 40 * time.Second},
		{"capped", true, 10 * time.Second, pollCeiling},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pollInterval(tt.watched, tt.cost); got != tt.want {
				t.Fatalf("pollInterval(%t, %v) = %v, want %v", tt.watched, tt.cost, got, tt.want)
			}
		})
	}
}
