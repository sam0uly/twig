# twig

<img width="1774" height="887" alt="Image" src="https://github.com/user-attachments/assets/2948ab96-17a6-4d1f-910c-074d5b0779ba" />

A diff viewer for the terminal, built for the repository you actually have open.
Files come as a flat list or a directory tree, with a tab for commits. The diff
has line numbers, word-level highlighting, split or unified layout, scrollbars
both ways, hunk jumps, search and smooth scrolling. You can stage, unstage and
copy patches without leaving it. If the tree is clean, a small twig grows where
the diff would be.

It talks to the repository directly through [go-git], so there is no `git` binary
to install or shell out to. The rest of the stack is [Bubble Tea v2], [Bubbles
v2], [Lip Gloss v2], [ultraviolet] and [harmonica], with a [cobra] CLI styled by
[fang] and configured through [viper].

## Install

```sh
go install github.com/sam0uly/twig@latest
```

Or build it yourself, with [Task] or plain Go:

```sh
task build
go build -o twig .
```

## Usage

```sh
twig              # unstaged changes
twig --staged     # staged changes
twig HEAD~2       # what a commit changed
twig main..HEAD   # what a range changed
twig log          # browse commits, open any of them
```

## Keys

| Key                                  | Action                                              |
| ------------------------------------ | --------------------------------------------------- |
| `enter`                              | open diff / jump to file / expand a tree folder     |
| `esc`                                | back to the sidebar                                 |
| `tab`                                | next pane                                           |
| `shift+tab`                          | previous pane                                       |
| `j` / `k`, `d` / `ctrl+u`, `f` / `b` | scroll the diff                                     |
| `h` / `l`, `←` / `→`                 | horizontal scroll (when wrap is off)                |
| `[` / `]`                            | previous / next hunk                                |
| `J` / `K`                            | next / previous file                                |
| `g` / `G`                            | top / bottom                                        |
| `t`                                  | toggle tree / flat file listing                     |
| `a`                                  | toggle all-changes view (every file in one scroll)  |
| `H` / `L`                            | hide or show the left / right sidebar               |
| `/`                                  | filter files (in the list) or search the diff       |
| `n` / `N`                            | next / previous search match                        |
| `s` / `u`                            | stage / unstage the selected file                   |
| `C`                                  | commit the staged changes (ctrl+s to confirm)       |
| `P`                                  | push to the remote                                  |
| `y` / `Y`                            | copy the file patch / current hunk to the clipboard |
| `v`                                  | toggle split / unified diff view                    |
| `w`                                  | toggle soft wrap                                    |
| `Z`                                  | toggle ignore-whitespace (reloads the diff)         |
| `c`                                  | cycle context lines                                 |
| `r`                                  | refresh                                             |
| `?`                                  | full help                                           |
| `q`                                  | quit                                                |

## Configuration

Twig reads `twig.yaml` from `$XDG_CONFIG_HOME/twig` or `~/.config/twig`, and
`--config` points it somewhere else. Every key is optional.

```yaml
context: 3
ignore-whitespace: false
wrap: false
line-numbers: true
sidebar-width: 32
smooth-scroll: true
log-limit: 200
split-view: false
```

## Development

```sh
task test   # go test -race
task lint   # golangci-lint
task run    # go run .
```

## How it works

The short version: twig reads the repository and diffs it itself, and it is built
for the case the usual tools handle badly, a working tree with millions of
changed lines in it. What follows is the reasoning behind the parts that are not
obvious, including the trade-offs they cost.

### One session, one walk

Twig decodes the index once per session, re-resolves HEAD when it moves, and
builds a stat-first snapshot per refresh. Diffs are keyed by content hash, so
switching files is a lookup rather than another `status`. The content, worktree
and diff caches are bounded by bytes instead of entry count, because an
entry-count bound lets a handful of large files hold hundreds of megabytes.

### Anchor first, diff the gaps

A whole-file Myers diff is the wrong shape for a big rewrite, so twig anchors
first. Identical 64-line blocks that occur once on each side are matched; the
gaps between them are diffed with the linear-space [Myers] engine under a cost
cap scaled like xdiff's; and only regions small enough to be exact get an exact
script. Whatever is still unmatched after a two-second budget is reported as a
wholesale replacement, which is always a valid script. The anchoring idea is
[Bentley–McIlroy]'s, the same one git credits as patience and histogram
matching. Bounding the work is what [difftastic] does with byte and graph limits,
and what [diff-match-patch] does with a timeout.

### Ids, not strings

Interned lines are keyed by a 64-bit [`maphash`] fingerprint and checked against
the line they came from, so a collision moves to the next probe slot instead of
silently equating two different lines. The map holds eight-byte keys and keeps no
copy of the source. A file that exists on only one side is not interned at all:
it is one hunk and one run.

### Runs, not rows

The rendered rows of a large diff are stored as runs (`Equal`, `Delete`,
`Insert`, `Replace`) with their source ranges and a prefix index for row lookup,
rather than a record per row. That is the same range-encoded script git's
[xdiff], [similar]'s `DiffOp` and [diff-match-patch] use. A file added whole is
two runs no matter how many lines it has; a normal code diff is three or four per
hunk. Drawing a row needs no stored counter, because the run carries the line it
starts at.

### Hunks that start where a reader would

Each hunk slides inside the context it can spare, never more than half of it, so
its edges land after a blank line or at a dedent. That is git's [indent
heuristic] applied to the context window. Word highlighting pairs changed lines
by token similarity rather than by position, and it gives up rather than
spending the frame: huge runs, very long lines, or a pass over 150 ms all keep
their plain wash.

### Documents, not slices

The all-changes view streams in blocks, keeps a [Fenwick] prefix index over them
and builds only the rows near the viewport, so thousands of changed files cost a
few ints each. The same machinery pages a single huge file in 2048-row blocks. A
search over a paged file scans the two source texts and maps a byte offset back
to its line, which is why it allocates a handful of objects instead of
formatting every row.

### The window comes first

Opening a repository and resolving HEAD can take seconds, so the branch label,
the status and the log all load as commands. The window is on screen first and
fills in. Loading the log does not open a commit either: on the files tab the
pane belongs to the file you are looking at, and a background load that finishes
late must not take it away.

### A commit is a list first

Selecting a commit asks for its changed paths, which is tree data and reads no
content. Small commits are then diffed whole, because the per-file counts are
worth it. A large one becomes a streaming document where each file is generated
when it scrolls into view, with its counts appearing as they arrive. If a file's
content is missing locally, a shallow or blobless partial clone or a submodule,
the file shows one band saying so instead of failing the commit. The same rule
covers any failed load: the pane shows the reason, never the previous file.

### No external git

Shelling out to `git diff` would inherit xdiff, rename detection and promisor
fetches for free, and it is deliberately not done. The cost is real and accepted:
in a blobless partial clone, history that is not present locally cannot be shown,
because go-git does not fetch on demand.

The upshot is that opening a file costs what changed, not what the file contains.
A file added whole is one hunk and a couple of runs however many lines it has, a
normal code diff is a few runs per hunk, and searching or scrolling a diff with
millions of rows touches only the rows on screen.

## Testing

The diff engine is checked three ways, and the third is the one that catches bad
pairing.

- **Invariants over a corpus.** `internal/diff/corpus_test.go` diffs every Go
  file in the repository against four deterministic mutations of itself, about a
  thousand pairs, and asserts properties that must hold for all of them: spans
  inside their line and on rune boundaries, only changed rows highlighted, and a
  pairing that is a partial bijection, order-preserving, above the similarity
  floor, and symmetric when the two sides are swapped.
- **Golden files.** `internal/diff/testdata/golden/` pins the full row and span
  layout of a curated set of cases, one per behaviour worth protecting, and
  records the total number of highlighted bytes so a noisier diff fails instead
  of quietly highlighting more. Regenerate with `go test ./internal/diff -update`
  after an intentional change.
- **An oracle.** `internal/diff/pairing_test.go` builds files with known edits,
  each line carrying an id that survives a modification and disappears with a
  deletion. The expected pairing is therefore known exactly, so cross-pairings
  and missed pairs are counted as errors rather than eyeballed.

## Benchmarks

These are development numbers from one laptop, not a promise. They were taken on
a 12-core i7-9850H (2.60 GHz) with Go 1.27.3 on linux/amd64, three runs each, and
the figures below are medians. Run-to-run spread on a busy machine is roughly
20%, so compare runs against each other rather than against this table.

```sh
go test ./internal/diff ./internal/ui/components/diffview \
    -run '^$' -bench . -benchmem -count=3
```

### Throughput

| Benchmark              | What it measures                                             | Time    | Bytes/op | Allocs/op |
| ---------------------- | ------------------------------------------------------------ | ------- | -------- | --------- |
| `GenerateLarge`        | 5 000 lines, 400 scattered edits, rows materialised           | 12.6 ms | 3.69 MB  | 19 698    |
| `GenerateLargePaged`   | the same diff as a paged plan (descriptors, no rendered rows) | 3.04 ms | 371 KB   | 56        |
| `GenerateWholeFileAdd` | a 100 000 line file added whole                               | 4.68 ms | 402 KB   | 14        |
| `ViewLargeDiff`        | 20 000 line diff, full 120×40 repaint after a jump            | 1.52 ms | 64 KB    | 1 230     |
| `ViewScrollLine`       | one new row per frame, the rest served from the row cache     | 0.36 ms | 24 KB    | 278       |

Two things worth reading off that table. Paging a diff costs a quarter of
materialising the same rows, which is why the viewer pages above 4096 rows. And a
100 000 line addition costs 14 allocations, because a file that exists on one
side is one hunk and two runs, whatever its length.

### Retained memory

What a finished diff keeps, as reported by `File.Bytes()` rather than guessed:

| Diff                     | Rows    | Hunks | Retained | Source text |
| ------------------------ | ------- | ----- | -------- | ----------- |
| 100 000 line addition    | 100 001 | 1     | 3.09 MB  | 2.69 MB     |
| 5 000 line rewrite       | 10 001  | 1     | 348 KB   | 308 KB      |

Peak RSS while producing those, measured against a 4.3 MB test-binary baseline:
15.7 MB for the 100 000 line addition and 15.2 MB for a 20 000 line rewrite that
keeps 40 001 rows resident. Both figures include the fixture's own source
strings, so treat them as an upper bound rather than the engine's share.

### Size and startup

| Measure                       | Value  |
| ----------------------------- | ------ |
| Binary, `CGO_ENABLED=1`       | 24.72 MB |
| Binary, `CGO_ENABLED=0`       | 24.65 MB |
| Warm process start (`--help`) | 8–10 ms  |

The binary is large because of the TUI and git stacks it embeds, not because of
the diff engine; the engine's own code is a few thousand lines.

## References

The engine and the document model borrow their shape from these. The code is
twig's own; the ideas and their trade-offs belong to the people who wrote them.

- [Eugene W. Myers, _An O(ND) Difference Algorithm and Its Variations_][Myers],
  the algorithm behind the exact regions, in its linear-space form.
- [Bentley & McIlroy, _A Technique for Isolating Differences Between Files_][Bentley–McIlroy],
  matching unique anchors first and recursing into the gaps.
- [git diff algorithms and the indent heuristic][indent heuristic]: `myers`,
  `minimal`, `patience`, `histogram`, and hunk boundary shifting.
- [git's xdiff implementation][xdiff]: bounded search, record hashing and
  `xdl_change_compact`.
- [The patience diff algorithm (Bram Cohen)][patience] and [JGit's histogram
  diff][histogram]: anchoring on low-occurrence lines.
- [difftastic]: hard byte and graph limits with a line-oriented fallback.
- [Gregory Pakosz, _diff-match-patch_][diff-match-patch]: range-encoded diffs,
  two-stage line diffing and a wall-clock timeout.
- [Nambiar & Apte, _similar_][similar]: `DiffOp`, algorithm selection, and
  deadlines for inline diffs.
- [Fenwick trees][Fenwick]: the prefix index that keeps row offsets stable while
  blocks are appended and evicted.

## Acknowledgments

Standing on the shoulders of [Charm], for [Bubble Tea v2], [Bubbles v2], [Lip
Gloss v2], [ultraviolet], [harmonica], [fang] and [tone]; [go-git], for reading
the repository without a git binary; [cobra] and [viper] for the CLI; and [Task]
for the build. Thanks also to the authors of the diff tools above, whose
documented trade-offs settled several arguments in this codebase.

[Bubble Tea v2]: https://github.com/charmbracelet/bubbletea
[Bubbles v2]: https://github.com/charmbracelet/bubbles
[Lip Gloss v2]: https://github.com/charmbracelet/lipgloss
[ultraviolet]: https://github.com/charmbracelet/ultraviolet
[harmonica]: https://github.com/charmbracelet/harmonica
[fang]: https://github.com/charmbracelet/fang
[tone]: https://github.com/charmbracelet/tone
[Charm]: https://charm.sh
[cobra]: https://github.com/spf13/cobra
[viper]: https://github.com/spf13/viper
[go-git]: https://github.com/go-git/go-git
[Task]: https://taskfile.dev
[Myers]: http://www.xmailserver.org/diff2.pdf
[Bentley–McIlroy]: https://dl.acm.org/doi/10.1145/359460.359467
[indent heuristic]: https://git-scm.com/docs/git-diff#Documentation/git-diff.txt---diff-algorithmpatienceminimalhistogrammyers
[xdiff]: https://github.com/git/git/tree/master/xdiff
[patience]: https://bramcohen.livejournal.com/73318.html
[histogram]: https://github.com/eclipse-jgit/jgit/blob/master/org.eclipse.jgit/src/org/eclipse/jgit/diff/HistogramDiff.java
[difftastic]: https://github.com/Wilfred/difftastic
[diff-match-patch]: https://github.com/google/diff-match-patch/wiki/API
[similar]: https://docs.rs/similar/latest/similar/
[Fenwick]: https://en.wikipedia.org/wiki/Fenwick_tree
[maphash]: https://pkg.go.dev/hash/maphash
