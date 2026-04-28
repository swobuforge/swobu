package main

import (
	"fmt"
	"os"

	"github.com/metrofun/swobu/internal/devtools/rolelint"
)

func main() {
	diagnostics, err := rolelint.Check(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(diagnostics) == 0 {
		return
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Filename != "" && diagnostic.Line > 0 {
			fmt.Fprintf(os.Stderr, "%s:%d:%d: %s\n", diagnostic.Filename, diagnostic.Line, diagnostic.Column, diagnostic.Message)
			continue
		}
		if diagnostic.Filename != "" {
			fmt.Fprintf(os.Stderr, "%s: %s\n", diagnostic.Filename, diagnostic.Message)
			continue
		}
		fmt.Fprintln(os.Stderr, diagnostic.Message)
	}
	os.Exit(1)
}
