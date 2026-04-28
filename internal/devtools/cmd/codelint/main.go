package main

import (
	"fmt"
	"os"

	"github.com/metrofun/swobu/internal/devtools/codelint"
)

func main() {
	diagnostics, err := codelint.Check(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(diagnostics) == 0 {
		return
	}

	for _, diagnostic := range diagnostics {
		if diagnostic.Filename != "" && diagnostic.Line > 0 {
			fmt.Fprintf(os.Stderr, "error: %s:%d:%d: %s\n", diagnostic.Filename, diagnostic.Line, diagnostic.Column, diagnostic.Message)
			continue
		}
		if diagnostic.Filename != "" {
			fmt.Fprintf(os.Stderr, "error: %s: %s\n", diagnostic.Filename, diagnostic.Message)
			continue
		}
		fmt.Fprintf(os.Stderr, "error: %s\n", diagnostic.Message)
	}
	os.Exit(1)
}
