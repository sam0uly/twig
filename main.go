package main

import (
	"context"
	"fmt"
	"os"

	"samouly.fun/twig/cmd"
)

func main() {
	if err := cmd.Execute(context.Background()); err != nil {
		// fang presents the error already; just exit with a failure code.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
