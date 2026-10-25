// Command openapi generates or checks the canonical target OpenAPI skeleton.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"github.com/swobuforge/swobu/internal/adapters/inbound/httpapi/apischema"
)

func main() {
	check := flag.Bool("check", false, "fail if output differs without writing")
	output := flag.String("output", "api/openapi.json", "output path")
	flag.Parse()
	raw, err := apischema.Document()
	if err != nil {
		fail(err)
	}
	if *check {
		existing, err := os.ReadFile(*output)
		if err != nil {
			fail(err)
		}
		if !bytes.Equal(existing, raw) {
			fail(fmt.Errorf("%s differs; run make openapi-generate", *output))
		}
		return
	}
	if err := os.WriteFile(*output, raw, 0644); err != nil {
		fail(err)
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
