<div align="left">
  <img
    src="https://github.com/user-attachments/assets/2948ab96-17a6-4d1f-910c-074d5b0779ba"
    alt="Twig logo"
    width="420"
    align="right"
  />

  <p style="margin: 0;">
    <strong>A live diff viewer for working trees bigger than your screen.</strong>
    It refreshes in about a tenth of a second while you edit, pages diffs with
    millions of rows instead of choking on them, stages, commits and pushes
    without leaving the pane, and ships as one 11 MB binary with no libgit2
    inside. If the tree is clean, a small twig grows where the diff would be.
  </p>
</div>

It drives `git` for repository state and diffs with its own engine, so ignore
rules, rename detection and index races behave like the `git` you already use.

## Why it's fast

|                          | twig                                            | the usual viewer                   |
| ------------------------ | ----------------------------------------------- | ---------------------------------- |
| a save                   | ~150 ms, watched, no repaint if nothing changed | manual refresh, or a full re-read  |
| a 100000 line file added | 4.7 ms, 14 allocations                          | seconds, hundreds of thousands     |
| a 5000 line rewrite      | 12.6 ms, 3.0 ms paged                           | quadratic on adversarial edits     |
| a screen of colour       | ~100 ns a line, 2.8 µs a screen, once, cached   | re-lexes the file, or colours none |
| idle                     | 0% CPU                                          | a timer that keeps re-reading      |

The engine anchors on unique blocks first and only runs Myers inside the gaps,
under a cost cap, so a rewrite that would pin a core for a minute is a few
milliseconds. Nothing is kept per row: a diff is a list of runs with a prefix
index, and a document only materialises the rows near the viewport.

## Install

```sh
go install samouly.fun/twig@latest
```

Or build it yourself, with [Task] or plain Go:

```sh
task                 # trimmed release build, version stamped from git
go build -trimpath -ldflags "-s -w" -o twig .
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

`H` and `L` bring a sidebar in; the diff gets the whole window otherwise.
Search and the list filters run as you type, and `esc` clears either.

| Key                      | Action                                          |
| ------------------------ | ----------------------------------------------- |
| `enter`                  | open diff / jump to file / expand a tree folder |
| `esc`                    | back to the sidebar                             |
| `tab` / `shift+tab`      | next / previous pane                            |
| `j` / `k`, `↑` / `↓`     | move the staging cursor a line                  |
| `f` / `b`, `pgdn`/`pgup` | move it a screenful (`ctrl+d` / `ctrl+u` too)   |
| `h` / `l`, `←` / `→`     | horizontal scroll (when wrap is off)            |
| `[` / `]`                | previous / next hunk, cursor and all            |
| `J` / `K`                | next / previous file                            |
| `g` / `G`                | first / last row a keystroke can act on         |
| `t`                      | tree / flat listing                             |
| `a`                      | all changes in one scroll                       |
| `H` / `L`                | left / right sidebar                            |
| `/`                      | filter files, or search the diff                |
| `n` / `N`                | next / previous match                           |
| `space`                  | stage or unstage what the cursor is on          |
| `V`                      | select a range, extended with `j` / `k`         |
| `z`                      | undo the last staging operation                 |
| `x`                      | discard what the cursor is on                   |
| `s` / `u`                | stage / unstage the whole file                  |
| `C` / `P`                | commit / push                                   |
| `y` / `Y`                | copy the patch / the hunk                       |
| `v` / `w`                | split view / soft wrap                          |
| `o`                      | fold / unfold metadata, hunk, or file           |
| `Z` / `c`                | ignore whitespace / cycle context               |
| `r` / `?` / `q`          | refresh / help / quit                           |

Drag across diff text with the mouse to select exact characters and copy them
on release; diff signs are omitted from the copied text. For line selection,
use `V`, extend with `j`/`k`, then `y` to copy.
In the commits tab a yellow rail marks the section being read: it rides
the window as you scroll, resting on the commit metadata, a file band or a
hunk header, and `o` folds the section it marks.

## Staging

Cursor picks the scope: a band is the file, a hunk header the hunk, a changed line
that line. `V` selects a range, `j` / `k` extend it, `V` or `esc` drops it, one file
at a time. View keys move the cursor and glide with `smooth-scroll`; the wheel
scrolls and drags the cursor back.

A partly staged file is listed twice, one row per diff, so a row shows what `space`
acts on. In `a` the block picks the file and its two bands name the side
(`── semi.txt · staged`) or a `side-labels` glyph. `space` writes, `z` reverses;
`stage-apply: select` marks, `enter` applies, `esc` clears; the `-` and `+` halves of
a replacement move together.

No patches: twig builds the new content, writes it with
[`hash-object`][git hash-object] `-w` and records it with
[`update-index`][git update-index] `--cacheinfo`, so git keeps the lock, the rename
and the objects ([index format][gitformat-index]). Whole file only: binary,
submodule, symlink, rename, newline-only, a diff over 4096 rows (it is
[paged](#why-its-fast), so its rows are not there to merge a selection against),
and on discard any file with a [clean filter][gitattributes] or line ending
conversion.

`x` discards the same three scopes after showing the lines, scrollable, saying where
in the list you are: `enter` discards, `esc` keeps, `y` stays yank, `z` undoes it.
The lines are re-checked at write time, so a moved file is refused, not half-applied.

## Config

`twig.json` from `$XDG_CONFIG_HOME/twig` or `~/.config/twig`; `--config` points
it elsewhere. Every key is optional.

```json
{
  "context": 3,
  "ignore-whitespace": false,
  "wrap": false,
  "line-numbers": true,
  "borderless": false,
  "watch": true,
  "follow": true,
  "watch-debounce": 120,
  "sidebar-width": 32,
  "show-left-sidebar": false,
  "show-right-sidebar": false,
  "smooth-scroll": true,
  "log-limit": 200,
  "split-view": false,
  "syntax": true,
  "stage": true,
  "stage-apply": "instant",
  "discard-confirm": "line",
  "side-labels": "text"
}
```

`watch` is the live view: a debounced change becomes one status run, and a
refresh that finds nothing new does not touch the screen. `watch-debounce` is
the quiet period in milliseconds, 120 by default. `show-left-sidebar` and
`show-right-sidebar` decide what the window opens with, and `H` / `L` write the
choice back. Events come from inotify, kqueue or `ReadDirectoryChangesW`, so a
save lands in about a tenth of a second; when they cannot carry the view the
footer says `polling` and a two second pass takes over.

`follow` puts the cursor on the changed line, a third down. Lines match on side,
number and text: a rewrite counts again, a deletion lands where it was. A burst
takes the last line of the last file written, every 250 ms; `a` patches one
block instead of rebuilding; staging and a keypress pause it; `false` redraws in
place as before.

`syntax` colours diff lines by what a token is: keywords, strings, comments,
numbers, names. It is on by default, needs no external tool and no grammar
files, and `false` returns the plain diff wash.

Set `borderless` to `true` in `twig.json` to remove every pane's frame and
separate adjacent panes with one blank column. Unfocused panes mute their item
colors; focused panes keep the full palette. This setting has no CLI flag.

`stage` adds the cursor, `false` gives `j`/`k` back. `stage-apply`: `instant`/`select`.
`discard-confirm` is the smallest discard that asks: `line`, `hunk` (one line goes),
`file` (only files), `never`. `side-labels` in `a`: `text`, `glyph` (`▲` index,
`◆` worktree), `off`.

## How it works

**One status run.** `git status --porcelain=v2` carries the index and HEAD blob
ids, so diffs are keyed by content ids: switching files is a lookup, and an
unchanged refresh costs no repaint. Caches are bounded by bytes, because an
entry-count bound lets a handful of huge files hold hundreds of megabytes.

**Anchors before Myers.** Identical 64-line blocks that occur once on each side
are matched, the gaps between them are diffed with the linear-space
[Myers algorithm][Myers] under a cost cap scaled like git's [xdiff], and only
regions small enough to be exact get an exact script. What remains after a
two-second budget is reported as a wholesale replacement, which is always a
valid script. The anchoring is [Bentley–McIlroy]'s, the same idea git credits as
patience and histogram matching; bounding the work is what [difftastic] and
[diff-match-patch] do in their own ways.

**Two shapes need no matching.** Content that only grew or shrank at its end is
one hunk of additions (or deletions) above the last context lines, so a file
something is appending to costs a scan of the shared head, not a diff. And a
region whose two sides hold no line in common can only be a wholesale
replacement: the interning pass already knows, so Myers is skipped. Both are
exact, not budget-driven, and the rows they produce are the ones the matcher
would have produced for the same script.

**Ids, runs, documents.** Lines are interned behind a 64-bit [`maphash`]
fingerprint and checked against the line they came from, so a collision probes
instead of equating two different lines. A paged diff is a list of runs
(`Equal`, `Delete`, `Insert`, `Replace`) with their source ranges and a
[Fenwick] prefix index, so a file added whole is two runs however long it is,
and a row needs no stored counter. A run answers the reverse question too, which
row paints a given source line, so a cursor placed from a line number costs no
walk. The all-changes view streams blocks, indexes them the same way, and builds
only the rows near the viewport; a chunk's diffs are independent and are built on
every core, with the repository reads kept on one goroutine. Searching a paged
file scans the two sources and maps a byte offset back to its line.

**Highlighting: tokens, not a tree.** A diff only needs a keyword told from a string,
a comment from a number, a key from a value: twig scans instead of parsing, one word of
state carrying a block comment, a raw string or half a tag across a hunk gap, 14
token classes for every language, and tables that are plain strings - no grammar
file, no regex engine, no embedded data. It costs 256 KB and about 100 ns a line,
2.8 µs for a 28 line screen, once per row. The model is
[Reps–Teitelbaum–Demers]'s incremental lexer, the same idea as Vim's `syn sync`
and [Monarch], the tokenizer vscode uses for the same reason. Colours follow the
[Charm][Charm] - [charmtone colorscheme][tone].

**It follows the file into other languages.** `<script>` and `<style>` take theirs
from `lang`, `.php` islands are PHP inside HTML, a Markdown fence takes its tag,
and the host takes the line back at the closer; heredocs and raw strings match a
per-file terminator; nested comments carry a depth; a regex shares JSX's "an
expression may start here" bit. Lines over 400 bytes stay plain.

<details>
<summary>64 tables, and what a scanner cannot express</summary>

Go, Rust, Python, JS/JSX, TS/TSX, C/C++, Objective-C, CUDA, D, Nim, Crystal, Zig,
Java, C#, Kotlin, Swift, Scala, Dart, Ruby, PHP, Groovy, Elixir, Erlang, Haskell,
Elm, OCaml, Clojure, F#, PureScript, Lisp, R, Julia, Perl, Raku, PowerShell, Lua,
shell, Batch, Awk, Tcl, Vim script, Fortran, LaTeX, SQL, JSON, YAML, TOML, INI,
HTML, Vue/Svelte, XML, CSS, Markdown, GraphQL, protobuf, HCL, Nix, Solidity,
Nginx, Verilog and assembly. Anything else keeps the plain wash, and adding one
is a table entry and a fixture, about 2 KB with no start-up cost.

A scanner cannot do: D's `/+ +/`, LaTeX `\commands`, Objective-C's `@interface`
prefix, YAML's `|` block scalars, Lua's levelled `--[==[` comments, Svelte's
`{#if}` blocks, JSX nested inside a `{expression}`, a single-line `<?php … ?>`.
Each degrades to plain text rather than wrong colour, and a hunk that starts
inside a block paints plain until a closer appears.

</details>

**Git does git.** Status, rename detection, ignore rules and the racy index are
delegated to the `git` binary through porcelain. A blobless partial clone still
works: `git cat-file` fetches on demand, and an object that is genuinely gone
degrades to one band saying so instead of failing the view.

**A commit is a list first.** Selecting one reads changed paths, which is tree
data and no content. Small commits are diffed whole; a large one becomes a
streaming document where each file is generated as it scrolls into view, with
counts filling in as they arrive.

**The window comes first.** Opening a repository can take seconds, so the branch
label, the status and the log all load as commands: the window is on screen and
fills in, and a slow commit load never takes the pane away from the file you are
reading.

## Testing

The engine is checked three ways: invariants over a corpus of ~1000 generated
pairs (spans on rune boundaries, pairing a partial bijection, order-preserving,
symmetric under swap), golden files that pin the exact row and span layout, and
an oracle that builds files with known edits so a missed or cross-pair is a
counted error. `go test ./internal/diff -update` regenerates the goldens. The
lexer is checked the same way: a span must sit inside its line and never overlap
another, and tokenising a file line by line must agree with tokenising it in one
gulp, which is what a lost state byte breaks.

## Benchmarks

One laptop, 12-core i7-9850H, Go 1.27.3, medians of three runs; treat the spread
as ~20% and compare runs rather than the table.

| Benchmark             | Time    | Allocations |
| --------------------- | ------- | ----------- |
| 5000 lines, 400 edits | 12.6 ms | 19 698      |
| the same, paged       | 3.04 ms | 56          |
| 100000 line addition  | 4.68 ms | 14          |
| 20000 row repaint     | 2.35 ms | 8 300       |
| one scroll step       | 0.34 ms | 505         |
| 28 rows of tokens     | 2.8 µs  | 0           |
| one row, coloured     | 53 µs   | 246         |

A 100000 line addition retains 3.09 MB for 2.69 MB of source, and paging costs
a quarter of materialising the same rows, which is why the viewer pages above
4096 rows.

## Development

```sh
task test   # go test -race
task lint   # gofmt, staticcheck, golangci-lint
task run    # go run .
```

CI runs gofumpt, vet on every release platform, the test suite with the race
detector, a release build under a 12 MB size guard, and a goreleaser snapshot so
a broken release config fails on a branch instead of during a tag. A `v*` tag
publishes the cross-compiled binaries, checksums and packages.

## Acknowledgments

Standing on the shoulders of [Charm] - [Bubble Tea v2], [Bubbles v2], [Lip Gloss
v2], [ultraviolet], [harmonica], [fang] and [tone] - and [Task] for the build.

The scanner follows the line-state model of [micro]'s highlighter. Heredoc
terminators borrow from Vim's `\z(` external matches and tree-sitter's external
scanners, and the JSX disambiguation from [TypeScript]'s own parser. [Chroma] and
[tree-sitter] were measured as alternatives before this was written: the first
costs 2.18 MB and cannot resume mid-file, the second 6.7 MB and cgo.

## License

Apache License, Version 2.0. See [LICENSE](LICENSE).

[Bubble Tea v2]: https://github.com/charmbracelet/bubbletea
[Bubbles v2]: https://github.com/charmbracelet/bubbles
[Lip Gloss v2]: https://github.com/charmbracelet/lipgloss
[ultraviolet]: https://github.com/charmbracelet/ultraviolet
[harmonica]: https://github.com/charmbracelet/harmonica
[fang]: https://github.com/charmbracelet/fang
[tone]: https://github.com/charmbracelet/tone
[Charm]: https://charm.land
[Task]: https://taskfile.dev
[Myers]: http://www.xmailserver.org/diff2.pdf
[Bentley–McIlroy]: https://dl.acm.org/doi/10.1145/359460.359467
[xdiff]: https://github.com/git/git/tree/master/xdiff
[difftastic]: https://github.com/Wilfred/difftastic
[diff-match-patch]: https://github.com/google/diff-match-patch/wiki/API
[Fenwick]: https://en.wikipedia.org/wiki/Fenwick_tree
[maphash]: https://pkg.go.dev/hash/maphash
[Reps–Teitelbaum–Demers]: https://dl.acm.org/doi/pdf/10.1145/2166.357218
[Monarch]: https://code.visualstudio.com/blogs/2017/02/08/syntax-highlighting-optimizations
[micro]: https://github.com/zyedidia/micro
[Chroma]: https://github.com/alecthomas/chroma
[tree-sitter]: https://github.com/tree-sitter/tree-sitter
[TypeScript]: https://github.com/microsoft/TypeScript
[gitformat-index]: https://git-scm.com/docs/gitformat-index
[gitattributes]: https://git-scm.com/docs/gitattributes
[git hash-object]: https://git-scm.com/docs/git-hash-object
[git update-index]: https://git-scm.com/docs/git-update-index
