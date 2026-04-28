package e2e_test

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Verifies real client binaries shipped in the container are runnable.
// This test intentionally executes binaries directly (not fake wrappers).
func TestClientBinaries_ContainerSmoke(t *testing.T) {
	tests := []struct {
		name   string
		binary string
		args   []string
	}{
		{name: "aider", binary: "aider", args: []string{"--version"}},
		{name: "opencode", binary: "opencode", args: []string{"--version"}},
		{name: "continue", binary: "cn", args: []string{"--version"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requireClientBinaryEvidence(t, tc.binary)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, tc.binary, tc.args...)
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("%s %s failed: %v stdout=%q stderr=%q", tc.binary, strings.Join(tc.args, " "), err, stdout.String(), stderr.String())
			}
			if strings.TrimSpace(stdout.String()) == "" && strings.TrimSpace(stderr.String()) == "" {
				t.Fatalf("%s %s returned no output", tc.binary, strings.Join(tc.args, " "))
			}
		})
	}
}
