package ui

import (
	"strconv"

	"samouly.fun/twig/internal/config"
	"samouly.fun/twig/internal/diff"
)

// cacheBytes bounds the diffs kept in memory while navigating, in the currency
// that actually runs out: a cached diff pins its rows, or the sources a paged
// diff rebuilds them from, for as long as it stays.
const cacheBytes = 64 << 20

// cacheLimit still caps the number of entries: thousands of tiny diffs cost
// more in bookkeeping than in bytes.
const cacheLimit = 128

// cacheEntry holds one cached diff. File diffs fill file; commit diffs fill
// both the aggregated document and a content-free listing of its files.
type cacheEntry struct {
	file  *diff.File
	files []*diff.File

	// bytes is what the entry keeps resident, filled in by Put.
	bytes int
}

// entryBytes estimates what an entry holds. A commit's per-file list is slim,
// so the aggregate is the only content counted.
func entryBytes(e cacheEntry) int {
	n := e.file.Bytes()
	for _, f := range e.files {
		n += f.Bytes()
	}
	return n
}

// diffCache is a small insertion-ordered cache. Re-visiting a file or a
// commit while scrolling a long list is then a map lookup instead of a full
// diff regeneration. limit is in bytes.
type diffCache struct {
	entries map[string]cacheEntry
	order   []string
	bytes   int
	limit   int
}

func newDiffCache(limit int) *diffCache {
	return &diffCache{entries: make(map[string]cacheEntry), limit: max(1, limit)}
}

// Get returns the cached entry for key.
func (c *diffCache) Get(key string) (cacheEntry, bool) {
	e, ok := c.entries[key]
	return e, ok
}

// Put stores an entry, evicting the oldest ones until the cache fits both its
// byte and its entry bound.
func (c *diffCache) Put(key string, e cacheEntry) {
	e.bytes = entryBytes(e)
	if e.bytes > c.limit {
		// A diff bigger than the whole budget would evict everything and then
		// itself. The viewer holds whatever it is showing anyway.
		return
	}

	if old, ok := c.entries[key]; ok {
		c.bytes -= old.bytes
	} else {
		c.order = append(c.order, key)
	}
	c.entries[key] = e
	c.bytes += e.bytes

	for len(c.order) > 0 && (c.bytes > c.limit || len(c.order) > cacheLimit) {
		oldest := c.order[0]
		c.order = c.order[1:]
		if v, ok := c.entries[oldest]; ok {
			c.bytes -= v.bytes
			delete(c.entries, oldest)
		}
	}
}

// Delete removes one entry.
func (c *diffCache) Delete(key string) {
	e, ok := c.entries[key]
	if !ok {
		return
	}
	c.bytes -= e.bytes
	delete(c.entries, key)
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// Clear drops every entry, e.g. after the working tree changed.
func (c *diffCache) Clear() {
	c.entries = make(map[string]cacheEntry)
	c.order = nil
	c.bytes = 0
}

// fileCacheKey identifies a file diff; the options are part of the key so a
// context or whitespace toggle never serves stale content.
func fileCacheKey(path string, staged bool, cfg config.Config) string {
	return "f\x00" + strconv.FormatBool(staged) +
		"\x00" + strconv.Itoa(cfg.Context) +
		"\x00" + strconv.FormatBool(cfg.IgnoreWhitespace) +
		"\x00" + path
}

// commitCacheKey identifies a commit diff.
func commitCacheKey(hash string, cfg config.Config) string {
	return "c\x00" + strconv.Itoa(cfg.Context) +
		"\x00" + strconv.FormatBool(cfg.IgnoreWhitespace) +
		"\x00" + hash
}
