package git

import (
	"strings"
	"testing"

	"samouly.fun/twig/internal/diff"
)

// TestWorkCacheBoundedByBytes checks that one oversized file cannot stay
// resident: the limit is on bytes, not on entries, because that is what the
// process actually runs out of.
func TestWorkCacheBoundedByBytes(t *testing.T) {
	const limit = 1024
	c := newWorkCache(limit)

	c.put("big", workEntry{data: strings.Repeat("x", limit+1)})
	if _, ok := c.get("big"); ok {
		t.Fatal("an entry larger than the limit must not be cached")
	}
	if c.bytes != 0 {
		t.Fatalf("bytes = %d, want 0", c.bytes)
	}

	for i := 0; i < 8; i++ {
		name := string(rune('a' + i))
		c.put(name, workEntry{data: strings.Repeat(name, 256)})
	}
	if c.bytes > limit {
		t.Fatalf("bytes = %d, over the %d limit", c.bytes, limit)
	}
	if _, ok := c.get("a"); ok {
		t.Fatal("the oldest entry should have been evicted")
	}
	if _, ok := c.get("h"); !ok {
		t.Fatal("the newest entry should still be cached")
	}
}

// TestWorkCacheReplacesEntry checks that updating a path does not double count
// its bytes nor queue it twice.
func TestWorkCacheReplacesEntry(t *testing.T) {
	c := newWorkCache(1024)
	c.put("a", workEntry{data: strings.Repeat("x", 512)})
	c.put("a", workEntry{data: strings.Repeat("y", 128)})
	if c.bytes != 128 {
		t.Fatalf("bytes = %d, want 128", c.bytes)
	}
	if len(c.order) != 1 {
		t.Fatalf("order = %v, want one entry", c.order)
	}
}

func TestWorkCacheReset(t *testing.T) {
	c := newWorkCache(1024)
	c.put("a", workEntry{data: "abc"})
	c.reset()
	if c.bytes != 0 || len(c.order) != 0 || len(c.m) != 0 {
		t.Fatalf("reset left state behind: %+v", c)
	}
}

// TestDiffCacheBoundedByBytes checks that generated diffs are evicted by size:
// a repository of big files must not keep every diff that was ever opened.
func TestDiffCacheBoundedByBytes(t *testing.T) {
	const limit = 10_000
	c := newDiffCache(limit)

	// Each file estimates at 100 rows * 40 bytes.
	newFile := func(path string) *diff.File {
		return &diff.File{NewPath: path, Lines: make([]diff.Line, 100)}
	}

	c.put("a", newFile("a.go"))
	c.put("b", newFile("b.go"))
	if c.bytes != 8000 {
		t.Fatalf("bytes = %d, want 8000", c.bytes)
	}

	c.put("c", newFile("c.go"))
	if _, ok := c.get("a"); ok {
		t.Fatal("the oldest diff should have been evicted")
	}
	if _, ok := c.get("c"); !ok {
		t.Fatal("the newest diff should be cached")
	}
	if c.bytes > limit {
		t.Fatalf("bytes = %d, over the %d limit", c.bytes, limit)
	}
}

// TestDiffCacheDropsOversized checks that one diff bigger than the whole budget
// is not kept: the viewer holds the file it is showing anyway, and caching it
// would pin the memory a second time.
func TestDiffCacheDropsOversized(t *testing.T) {
	c := newDiffCache(1000)
	c.put("huge", &diff.File{Lines: make([]diff.Line, 1000)})
	if _, ok := c.get("huge"); ok {
		t.Fatal("an oversized diff must not be cached")
	}
	if c.bytes != 0 {
		t.Fatalf("bytes = %d, want 0", c.bytes)
	}
}

// TestDiffCacheKeepsExistingEntry checks that re-putting a key does not charge
// its bytes twice.
func TestDiffCacheKeepsExistingEntry(t *testing.T) {
	c := newDiffCache(10_000)
	c.put("a", &diff.File{Lines: make([]diff.Line, 10)})
	c.put("a", &diff.File{Lines: make([]diff.Line, 10)})
	if c.bytes != 400 {
		t.Fatalf("bytes = %d, want 400", c.bytes)
	}
	if len(c.order) != 1 {
		t.Fatalf("order = %v, want one entry", c.order)
	}
}
