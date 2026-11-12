package cmd

import (
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"

	"samouly.fun/twig/internal/diff"
	"samouly.fun/twig/internal/output"
	"samouly.fun/twig/internal/ui"
)

// newPagerCommand reads the patch git would page.
func newPagerCommand(o *options) *cobra.Command {
	return &cobra.Command{
		Use:   "pager",
		Short: "Paint a patch from standard input",
		Long: `Read a diff on standard input and draw it, or print it when there is no
terminal to draw on. Anything that is not a patch is written out unchanged, so
twig is safe behind git.`,
		Example: `
  git config --global pager.diff "twig pager"
  git show HEAD | twig pager
`,
		Args: cobra.ArbitraryArgs,
		RunE: func(c *cobra.Command, args []string) error {
			tty := isTerminal(os.Stdout)
			color, err := resolveColor(o.color, tty)
			if err != nil {
				return err
			}
			if isTerminal(os.Stdin) {
				return errors.New("pager reads a diff on standard input")
			}

			in, err := io.ReadAll(c.InOrStdin())
			if err != nil {
				return err
			}
			out := c.OutOrStdout()

			// A patch can arrive already painted.
			d := diff.Parse(string(in))
			if len(d.Files) == 0 {
				d = diff.Parse(output.StripANSI(string(in)))
			}
			switch {
			case len(d.Files) == 0:
			case tty:
				cfg, _, err := o.loadConfig(c)
				if err != nil {
					return err
				}
				return ui.Run(c.Context(), ui.Options{Config: cfg, Document: d})
			case d.Combined():
				// One column per parent has no plain patch form to paint.
			case color:
				_, err = io.WriteString(out, output.Patch(d.Files, true))
				return err
			}
			_, err = out.Write(in)
			return err
		},
	}
}
