package main

import (
	"context"
	"os"

	"github.com/swobuforge/swobu/internal/adapters/inbound/cli"
)

func main() {
	runner := cli.Runner{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	os.Exit(int(runner.Run(context.Background(), os.Args[1:])))
}
