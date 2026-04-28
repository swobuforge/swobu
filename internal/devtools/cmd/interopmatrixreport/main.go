package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/metrofun/swobu/internal/devtools/interopmatrix"
)

func main() {
	var format string
	var outPath string
	var strict bool
	flag.StringVar(&format, "format", "text", "output format: text or json")
	flag.StringVar(&outPath, "out", "", "optional output file path")
	flag.BoolVar(&strict, "strict", false, "fail when declared compatibility cases are failing or untested")
	flag.Parse()

	report := interopmatrix.Build()
	var body []byte
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		raw, err := interopmatrix.JSON(report)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		body = raw
	default:
		body = []byte(interopmatrix.Text(report))
	}

	if strings.TrimSpace(outPath) != "" {
		abs := filepath.Clean(outPath)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := os.WriteFile(abs, append(body, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Println(string(body))

	if strict {
		if err := interopmatrix.Gate(report); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
