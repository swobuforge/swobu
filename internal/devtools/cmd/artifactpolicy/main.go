package main

import (
	"fmt"
	"os"

	"github.com/metrofun/swobu/internal/devtools/artifactpolicy"
)

func main() {
	root := "."
	if len(os.Args) > 1 && os.Args[1] != "" {
		root = os.Args[1]
	}
	diagnostics, err := artifactpolicy.Check(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(diagnostics) == 0 {
		fmt.Println("artifact policy checks passed")
		return
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Filename != "" {
			fmt.Fprintf(os.Stderr, "error: %s: %s\n", diagnostic.Filename, diagnostic.Message)
			continue
		}
		fmt.Fprintf(os.Stderr, "error: %s\n", diagnostic.Message)
	}
	os.Exit(1)
}
