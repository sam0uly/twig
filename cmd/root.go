// Package cmd is twig's command line.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/fang"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"samouly.fun/twig/internal/config"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/output"
	"samouly.fun/twig/internal/ui"
	"samouly.fun/twig/internal/version"
)

type options struct {
	cfgPath   string
	dirs      []string
	color     string
	noWatch   bool
	exitCode  bool
	patch     bool
	staged    bool
	unified   int
	ignoreWS  bool
	wrap      bool
	lineNos   bool
	sideWidth int
	logLimit  int
}

// New builds the twig root command.
func New() *cobra.Command {
	o := &options{}

	root := &cobra.Command{
		Use:   "twig [ref | base..head] [-- path...]",
		Short: "Read a git diff in the terminal",
		Long: `Read a worktree, a commit or a range: highlighted, searchable, and
stageable as you read. Piped to another program, twig prints the diff.`,
		Example: `
  twig
  twig --staged
  twig HEAD~2
  twig main..HEAD
  twig --staged -- internal/
  twig -C ~/src/app log
`,
		Args:              arguments(),
		ValidArgsFunction: completeRefs,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			return changeDir(o.dirs)
		},
		RunE: func(c *cobra.Command, args []string) error {
			return run(c.Context(), c, o, args)
		},
	}

	root.AddCommand(
		newLogCommand(o),
		newPagerCommand(o),
	)
	addRootFlags(root, o)

	return root
}

func run(ctx context.Context, c *cobra.Command, o *options, args []string) error {
	revs, paths := splitAtDash(c, args)
	src, err := resolveSource(revs, o)
	if err != nil {
		return err
	}
	src.Paths = paths

	cfg, path, err := o.loadConfig(c)
	if err != nil {
		return err
	}

	tty := isTerminal(os.Stdout)
	color, err := resolveColor(o.color, tty)
	if err != nil {
		return err
	}

	if o.patch || !tty {
		return printDiff(c, src, cfg, color, o.exitCode)
	}
	if o.exitCode {
		return errors.New("--exit-code needs --patch or a pipe")
	}
	if err := git.CheckRepository(); err != nil {
		return describe(err, src)
	}

	return ui.Run(ctx, ui.Options{
		Config:     cfg,
		ConfigPath: path,
		Source:     src,
	})
}

// loadConfig reads the config file and folds the flags over it.
func (o *options) loadConfig(c *cobra.Command) (config.Config, string, error) {
	resolved, err := config.Path(o.cfgPath)
	if err != nil {
		return config.Default(), "", err
	}
	if o.cfgPath != "" {
		// A path the reader named has to exist; the others may not.
		if _, err := os.Stat(resolved); err != nil {
			return config.Default(), resolved, fmt.Errorf("read config: %w", err)
		}
	}
	cfg, err := config.Load(resolved)
	if err != nil {
		return config.Default(), resolved, err
	}

	for _, flag := range []struct {
		name  string
		apply func()
	}{
		{"unified", func() { cfg.Context = o.unified }},
		{"ignore-whitespace", func() { cfg.IgnoreWhitespace = o.ignoreWS }},
		{"wrap", func() { cfg.Wrap = o.wrap }},
		{"line-numbers", func() { cfg.LineNumbers = o.lineNos }},
		{"sidebar-width", func() { cfg.SidebarWidth = o.sideWidth }},
		{"no-watch", func() { cfg.Watch = false }},
	} {
		if c.Flags().Changed(flag.name) {
			flag.apply()
		}
	}
	return cfg, resolved, nil
}

func addRootFlags(root *cobra.Command, o *options) {
	pf := root.PersistentFlags()
	pf.StringArrayVarP(&o.dirs, "chdir", "C", nil, "run in this directory (repeatable)")
	pf.StringVar(&o.cfgPath, "config", "", "config file path")
	pf.StringVar(&o.color, "color", "auto", "color the text output")

	f := root.Flags()
	f.BoolVarP(&o.staged, "staged", "s", false, "show staged changes")
	f.IntVarP(&o.unified, "unified", "U", 0, "context lines around hunks")
	f.BoolVarP(&o.ignoreWS, "ignore-whitespace", "w", false, "ignore whitespace changes")
	f.BoolVar(&o.wrap, "wrap", false, "wrap long lines")
	f.BoolVar(&o.lineNos, "line-numbers", false, "show line numbers")
	f.IntVar(&o.sideWidth, "sidebar-width", 0, "width of the file list")
	f.BoolVar(&o.patch, "patch", false, "print the diff, no viewer")
	f.BoolVar(&o.exitCode, "exit-code", false, "exit 1 when there is a diff")
	f.BoolVar(&o.noWatch, "no-watch", false, "do not follow changes on disk")

	must(root.MarkPersistentFlagFilename("config", "json"))
	must(root.RegisterFlagCompletionFunc("color", completeChoice("auto", "always", "never")))
}

func Execute(ctx context.Context) error {
	return fang.Execute(
		ctx, New(),
		fang.WithVersion(version.Number()),
		fang.WithCommit(version.CommitShort()),
		fang.WithErrorHandler(errorHandler),
	)
}

// errorHandler swallows errDiffFound: --exit-code has said it with its status.
func errorHandler(w io.Writer, st fang.Styles, err error) {
	if errors.Is(err, errDiffFound) {
		return
	}
	fang.DefaultErrorHandler(w, st, err)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// splitAtDash splits refs from paths, the way git does.
func splitAtDash(c *cobra.Command, args []string) (revs, paths []string) {
	at := c.ArgsLenAtDash()
	if at < 0 {
		return args, nil
	}
	if at > len(args) {
		at = len(args)
	}
	return args[:at], args[at:]
}

func arguments() cobra.PositionalArgs {
	return func(c *cobra.Command, args []string) error {
		revs, paths := splitAtDash(c, args)
		if len(revs) > 2 {
			return fmt.Errorf("at most two refs, got %d", len(revs))
		}
		for _, path := range paths {
			if path == "" {
				return errors.New("empty path after --")
			}
		}
		return nil
	}
}

func resolveSource(args []string, o *options) (git.Source, error) {
	if o.staged {
		if len(args) > 0 {
			return git.Source{}, errors.New("a staged view takes no ref")
		}
		return git.Source{Kind: git.SourceStaged}, nil
	}

	switch len(args) {
	case 0:
		return git.Source{Kind: git.SourceWorktree}, nil
	case 1:
		ref := args[0]
		// A three-dot range is git's to resolve: the merge base is not ours.
		if strings.Contains(ref, "..") && !strings.Contains(ref, "...") {
			from, to, _ := strings.Cut(ref, "..")
			if from == "" || to == "" {
				return git.Source{}, fmt.Errorf("range %q needs a ref on each side", ref)
			}
			return git.Source{Kind: git.SourceRange, Ref1: from, Ref2: to}, nil
		}
		return git.Source{Kind: git.SourceCommit, Ref1: ref}, nil
	default:
		if args[0] == "" || args[1] == "" {
			return git.Source{}, errors.New("a range needs a ref on each side")
		}
		return git.Source{Kind: git.SourceRange, Ref1: args[0], Ref2: args[1]}, nil
	}
}

func completeRefs(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) >= 2 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	refs, err := git.RefNames()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, ref := range refs {
		if strings.HasPrefix(ref, toComplete) {
			out = append(out, ref)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func completeChoice(values ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		var out []string
		for _, value := range values {
			if strings.HasPrefix(value, toComplete) {
				out = append(out, value)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

var errDiffFound = errors.New("differences found")

func isTerminal(f *os.File) bool { return term.IsTerminal(f.Fd()) }

func resolveColor(mode string, tty bool) (bool, error) {
	switch mode {
	case "never":
		return false, nil
	case "always":
		return true, nil
	case "", "auto":
		if _, set := os.LookupEnv("NO_COLOR"); set {
			return false, nil
		}
		return tty, nil
	default:
		return false, fmt.Errorf("unknown color mode %q: use auto, always or never", mode)
	}
}

func printDiff(c *cobra.Command, src git.Source, cfg config.Config, color, exitCode bool) error {
	if err := git.CheckRepository(); err != nil {
		return describe(err, src)
	}
	d, err := git.Diff(src, git.Options{Context: cfg.Context, IgnoreWhitespace: cfg.IgnoreWhitespace})
	if err != nil {
		return describe(err, src)
	}

	if text := output.Patch(d.Files, color); text != "" {
		if _, err := io.WriteString(c.OutOrStdout(), text); err != nil {
			return err
		}
	}
	if exitCode && len(d.Files) > 0 {
		return errDiffFound
	}
	return nil
}

// describe says what the reader can do about a failure.
func describe(err error, src git.Source) error {
	switch {
	case errors.Is(err, git.ErrNotARepository):
		return fmt.Errorf("%w (use -C to name a repository)", err)
	case errors.Is(err, git.ErrUnknownRef):
		return fmt.Errorf("no such ref %q", src.Describe())
	default:
		return err
	}
}

func changeDir(dirs []string) error {
	for _, dir := range dirs {
		if err := os.Chdir(dir); err != nil {
			return fmt.Errorf("cannot change to %q: %w", dir, err)
		}
	}
	return nil
}
