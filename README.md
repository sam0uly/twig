# twig

A clean git diff viewer for the terminal. Browse changed files
in a sidebar, read the diff with line numbers, hunk navigation, search and
smooth scrolling, and stage, unstage or yank patches without leaving the
keyboard.

Built with the Charm stack: [Bubble Tea v2], [Bubbles v2], [Lip Gloss v2],
[ultraviolet] and [harmonica], wrapped in a [cobra] CLI styled by [fang] and
configured with [viper].

## Install

```sh
go install github.com/yourname/twig@latest
```

Or build from source with [Task]:

```sh
task build
```

## Usage

```sh
twig              # unstaged changes in the working tree
twig --staged     # staged changes
twig HEAD~2       # changes introduced by a commit
twig main..HEAD   # changes between two refs
twig log          # browse recent commits and view any of them
```

## Keys

| Key                                  | Action                                              |
| ------------------------------------ | --------------------------------------------------- |
| `tab` / `shift+tab`                  | move focus between the file list and the diff       |
| `j` / `k`, `d` / `ctrl+u`, `f` / `b` | scroll the diff                                     |
| `h` / `l`                            | horizontal scroll (wrap off)                        |
| `[` / `]`                            | previous / next hunk                                |
| `J` / `K`                            | next / previous file                                |
| `g` / `G`                            | top / bottom                                        |
| `/`                                  | filter files (in the list) or search the diff       |
| `n` / `N`                            | next / previous search match                        |
| `s` / `u`                            | stage / unstage the selected file                   |
| `y` / `Y`                            | copy the file patch / current hunk to the clipboard |
| `W`                                  | toggle soft wrap                                    |
| `Z`                                  | toggle ignore-whitespace (reloads the diff)         |
| `c`                                  | cycle context lines                                 |
| `L`                                  | open the commit browser                             |
| `R`                                  | refresh                                             |
| `?`                                  | full help                                           |
| `q`                                  | quit                                                |

## Configuration

`twig` reads `twig.yaml` from `$XDG_CONFIG_HOME/twig` or `~/.config/twig`;
`--config` overrides the path.

```yaml
context: 3
ignore-whitespace: false
wrap: false
line-numbers: true
sidebar-width: 32
smooth-scroll: true
log-limit: 200
```

## Development

```sh
task test   # go test -race
task lint   # golangci-lint
task run    # go run .
```

[Bubble Tea v2]: https://github.com/charmbracelet/bubbletea
[Bubbles v2]: https://github.com/charmbracelet/bubbles
[Lip Gloss v2]: https://github.com/charmbracelet/lipgloss
[ultraviolet]: https://github.com/charmbracelet/ultraviolet
[harmonica]: https://github.com/charmbracelet/harmonica
[cobra]: https://github.com/spf13/cobra
[fang]: https://github.com/charmbracelet/fang
[viper]: https://github.com/spf13/viper
[Task]: https://taskfile.dev
