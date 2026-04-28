package main

import (
	"fmt"
	"os"

	"github.com/metrofun/swobu/internal/devtools/docpolicy"
)

func main() {
	root := "."
	if len(os.Args) > 1 && os.Args[1] != "" {
		root = os.Args[1]
	}
	diagnostics, err := docpolicy.Check(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(diagnostics) == 0 {
		fmt.Println("doc policy checks passed")
		return
	}

	exitCode := 0
	for _, diagnostic := range diagnostics {
		prefix := "error"
		if diagnostic.Warning {
			prefix = "warning"
		} else {
			exitCode = 1
		}
		if diagnostic.Filename != "" {
			fmt.Fprintf(os.Stderr, "%s: %s: %s\n", prefix, diagnostic.Filename, diagnostic.Message)
			continue
		}
		fmt.Fprintf(os.Stderr, "%s: %s\n", prefix, diagnostic.Message)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	fmt.Println("doc policy checks passed")
}
