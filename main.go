package main

import (
	"context"
	"os"

	"samouly.fun/twig/cmd"
)

func main() {
	if err := cmd.Execute(context.Background()); err != nil {
		os.Exit(1)
	}
}
