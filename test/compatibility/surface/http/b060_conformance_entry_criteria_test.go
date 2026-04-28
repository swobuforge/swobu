package http_test

import (
	"os/exec"
	"testing"
)

func TestB060_ExternalConformanceLanesPass(t *testing.T) {
	for _, lane := range []string{
		"./test/compatibility/runtime/openai",
		"./test/compatibility/runtime/anthropic",
	} {
		cmd := exec.Command("go", "test", lane)
		cmd.Dir = repoRootFromWD(t)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("conformance lane %q failed: %v\n%s", lane, err, string(out))
		}
	}
}
