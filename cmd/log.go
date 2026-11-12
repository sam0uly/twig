package cmd

import (
	"github.com/spf13/cobra"

	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/ui"
)

// newLogCommand opens the viewer on the commit browser.
func newLogCommand(o *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "log",
		Short:   "Browse commits",
		Long:    "The recent commits on the left, the one under the cursor on the right.",
		Example: "  twig log\n  twig log -n 200",
		Args:    cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			cfg, path, err := o.loadConfig(c)
			if err != nil {
				return err
			}
			if err := git.CheckRepository(); err != nil {
				return describe(err, git.Source{})
			}
			return ui.Run(c.Context(), ui.Options{
				Config:        cfg,
				ConfigPath:    path,
				Source:        git.Source{Kind: git.SourceWorktree},
				ShowLogPicker: true,
			})
		},
	}

	cmd.Flags().IntVarP(&o.logLimit, "limit", "n", 0, "how many commits to load")
	return cmd
}
