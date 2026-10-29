// Package cmd defines the twig command line interface, wrapped by fang for
// help, version and error presentation.
package cmd

import (
	"context"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"samouly.fun/twig/internal/config"
	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/ui"
)

// Build information, overridable with -ldflags.
var (
	version = "dev"
	commit  = "none"
)

// New builds the twig root command.
func New() *cobra.Command {
	var (
		cfgPath       string
		staged        bool
		unified       int
		ignoreWS      bool
		wrap          bool
		lineNumbers   bool
		sidebarWidth  int
		logLimit      int
		showLogPicker bool
	)

	root := &cobra.Command{
		Use:   "twig [base-ref] [head-ref]",
		Short: "A clean, glow-inspired git diff viewer",
		Long: `twig is a clean, glow-inspired git diff viewer for the terminal.

Browse changed files, read the diff with line numbers and hunk navigation,
search, stage and unstage files, and copy patches to the clipboard.

  twig              show unstaged changes in the working tree
  twig --staged     show staged changes
  twig HEAD~2       show the changes introduced by HEAD~2
  twig main..HEAD   show the changes between two refs
  twig log          browse recent commits and view any of them`,
		Args:              cobra.MaximumNArgs(2),
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := loadConfig(cfgPath)
			if err != nil {
				return err
			}

			// Command line flags override the config file.
			if c.Flags().Changed("unified") {
				cfg.Context = unified
			}
			if c.Flags().Changed("ignore-whitespace") {
				cfg.IgnoreWhitespace = ignoreWS
			}
			if c.Flags().Changed("wrap") {
				cfg.Wrap = wrap
			}
			if c.Flags().Changed("line-numbers") {
				cfg.LineNumbers = lineNumbers
			}
			if c.Flags().Changed("sidebar-width") {
				cfg.SidebarWidth = sidebarWidth
			}
			if c.Flags().Changed("log-limit") {
				cfg.LogLimit = logLimit
			}

			if err := git.CheckRepository(); err != nil {
				return err
			}

			src := git.Source{Kind: git.SourceWorktree}
			switch {
			case staged:
				src.Kind = git.SourceStaged
			case !showLogPicker && len(args) == 1:
				src = git.Source{Kind: git.SourceCommit, Ref1: args[0]}
			case !showLogPicker && len(args) == 2:
				src = git.Source{Kind: git.SourceRange, Ref1: args[0], Ref2: args[1]}
			}

			return ui.Run(c.Context(), ui.Options{
				Config:        cfg,
				Source:        src,
				ShowLogPicker: showLogPicker,
			})
		},
	}

	logCmd := &cobra.Command{
		Use:   "log",
		Short: "Browse recent commits and view any of them",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			cfg, err := loadConfig(cfgPath)
			if err != nil {
				return err
			}

			if err := git.CheckRepository(); err != nil {
				return err
			}

			return ui.Run(c.Context(), ui.Options{
				Config:        cfg,
				Source:        git.Source{Kind: git.SourceWorktree},
				ShowLogPicker: true,
			})
		},
	}
	root.AddCommand(logCmd)

	logCmd.Flags().IntVarP(&logLimit, "limit", "n", 0, "number of commits to load (overrides config)")

	root.PersistentFlags().StringVar(&cfgPath, "config", "", "config file path (default: $XDG_CONFIG_HOME/twig/twig.yaml)")
	root.Flags().BoolVarP(&staged, "staged", "s", false, "show staged changes instead of the working tree")
	root.Flags().IntVarP(&unified, "unified", "U", 0, "number of context lines around hunks (overrides config)")
	root.Flags().BoolVarP(&ignoreWS, "ignore-whitespace", "w", false, "ignore whitespace changes when diffing")
	root.Flags().BoolVar(&wrap, "wrap", false, "soft-wrap long lines in the diff view (overrides config)")
	root.Flags().BoolVar(&lineNumbers, "line-numbers", false, "show line numbers in the diff gutter (overrides config)")
	root.Flags().IntVar(&sidebarWidth, "sidebar-width", 0, "width of the file list sidebar (overrides config)")
	root.Flags().BoolVarP(&showLogPicker, "log", "l", false, "open the commit browser on startup")

	return root
}

func loadConfig(path string) (config.Config, error) {
	return config.Load(path)
}

// Execute runs the root command through fang, which handles help, version,
// completions, man pages and error presentation.
func Execute(ctx context.Context) error {
	return fang.Execute(
		ctx, New(),
		fang.WithVersion(version),
		fang.WithCommit(commit),
	)
}
