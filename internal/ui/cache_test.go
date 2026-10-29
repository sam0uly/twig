package ui

import (
	"strings"
	"testing"

	"twig/internal/diff"
	"twig/internal/git"
)

// paddedFile is a diff whose estimate is easy to predict: 100 rows at the
// per-row charge of the size estimate.
func paddedFile(path string) *diff.File {
	return &diff.File{NewPath: path, Lines: make([]diff.Line, 100)}
}

// TestDiffCacheBoundedByBytes checks that the navigation cache evicts by size,
// so stepping through many large files cannot pin all of them.
func TestDiffCacheBoundedByBytes(t *testing.T) {
	const limit = 10_000
	c := newDiffCache(limit)

	c.Put("a", cacheEntry{file: paddedFile("a.go")})
	c.Put("b", cacheEntry{file: paddedFile("b.go")})
	if c.bytes != 8000 {
		t.Fatalf("bytes = %d, want 8000", c.bytes)
	}

	c.Put("c", cacheEntry{file: paddedFile("c.go")})
	if _, ok := c.Get("a"); ok {
		t.Fatal("the oldest diff should have been evicted")
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatal("the newest diff should be cached")
	}
	if c.bytes > limit {
		t.Fatalf("bytes = %d, over the %d limit", c.bytes, limit)
	}
}

// TestDiffCacheEntryCap checks the second bound: thousands of tiny diffs cost
// more in bookkeeping than in bytes.
func TestDiffCacheEntryCap(t *testing.T) {
	c := newDiffCache(cacheBytes)
	for i := 0; i <= cacheLimit; i++ {
		c.Put(string(rune('a'+i%26))+string(rune('0'+i/26)), cacheEntry{file: &diff.File{}})
	}
	if len(c.order) > cacheLimit {
		t.Fatalf("kept %d entries, want at most %d", len(c.order), cacheLimit)
	}
}

// TestDiffCacheReplacesEntry checks that re-putting a key does not charge its
// bytes twice nor queue it again.
func TestDiffCacheReplacesEntry(t *testing.T) {
	c := newDiffCache(10_000)
	c.Put("a", cacheEntry{file: paddedFile("a.go")})
	c.Put("a", cacheEntry{file: paddedFile("a.go")})
	if c.bytes != 4000 {
		t.Fatalf("bytes = %d, want 4000", c.bytes)
	}
	if len(c.order) != 1 {
		t.Fatalf("order = %v, want one key", c.order)
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("the entry should still be cached")
	}
}

// TestDiffCacheDeleteAndClear keep the byte count honest when entries leave.
func TestDiffCacheDeleteAndClear(t *testing.T) {
	c := newDiffCache(10_000)
	c.Put("a", cacheEntry{file: paddedFile("a.go")})
	c.Put("b", cacheEntry{file: paddedFile("b.go")})
	c.Delete("a")
	if c.bytes != 4000 {
		t.Fatalf("bytes after delete = %d, want 4000", c.bytes)
	}
	c.Delete("missing")
	if c.bytes != 4000 {
		t.Fatalf("deleting an unknown key changed the count: %d", c.bytes)
	}
	c.Clear()
	if c.bytes != 0 || len(c.order) != 0 || len(c.entries) != 0 {
		t.Fatalf("clear left state behind: %+v", c)
	}
}

// TestCommitEntryCountsOnlyTheDocument pins that a cached commit charges its
// aggregated document and not the per-file payload it was built from.
func TestCommitEntryCountsOnlyTheDocument(t *testing.T) {
	files := []*diff.File{
		{NewPath: "a.go", Lines: make([]diff.Line, 100), Additions: 3},
		{NewPath: "b.go", Lines: make([]diff.Line, 100), Deletions: 2},
	}
	doc := diff.Aggregate(files)

	full := cacheEntry{file: doc, files: files}
	slim := cacheEntry{file: doc, files: slimFiles(files)}
	if entryBytes(slim) >= entryBytes(full) {
		t.Fatalf("slim entry %d bytes, want below %d", entryBytes(slim), entryBytes(full))
	}

	// The sidebar of a cached commit still has what it renders.
	for i, f := range slim.files {
		if f.Path() != files[i].Path() || f.Additions != files[i].Additions || f.Deletions != files[i].Deletions {
			t.Fatalf("file %d lost its identity: %+v", i, f)
		}
	}
}

// TestCachedCommitRenders drives the cache-hit path for a commit: the document
// comes from the cached aggregate and the sidebar from the slim listing, with
// nothing reloaded.
func TestCachedCommitRenders(t *testing.T) {
	m := testModel(120, 24)
	m.files.SetTab(1)
	m.setSize()

	files := []*diff.File{
		diff.Generate("a.go", diff.Modified, "a\n", "b\n", diff.GenerateOptions{Context: 3}),
		diff.Generate("sub/b.go", diff.Modified, "a\n", "c\n", diff.GenerateOptions{Context: 3}),
	}
	c := git.Commit{Hash: "h", Short: "abc1234", Subject: "subject"}
	m.cache.Put(commitCacheKey(c.Hash, m.cfg), cacheEntry{file: diff.Aggregate(files), files: slimFiles(files)})

	next, cmd := m.showCommit(c)
	if cmd != nil {
		t.Fatal("a cached commit should not be loaded again")
	}
	m = next.(model)
	if m.view.Empty() {
		t.Fatal("the cached commit document is empty")
	}
	if len(m.commitFileList) != len(files) {
		t.Fatalf("sidebar has %d files, want %d", len(m.commitFileList), len(files))
	}
	view := m.files.CommitFilesView()
	for _, f := range files {
		if !strings.Contains(view, f.Path()) {
			t.Fatalf("sidebar is missing %q:\n%s", f.Path(), view)
		}
	}
	assertGeometry(t, m, "cached commit")
}
