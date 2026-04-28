package cli_test

import (
	"strings"
	"testing"
)

func TestB071_InteractiveEntrypointContract(t *testing.T) {
	out, exitCode := runSwobu(t)
	if exitCode == 0 {
		t.Fatalf("exit code = %d, want non-zero in non-interactive mode; out=%s", exitCode, out)
	}
	if !strings.Contains(out, "swobu status") {
		t.Fatalf("output = %q, want guidance to use `swobu status`", out)
	}
}
