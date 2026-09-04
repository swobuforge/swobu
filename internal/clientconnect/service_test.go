package clientconnect

import (
	"testing"
	"time"
)

func TestRunLocalCommandDoesNotWaitForDescendantOutputHandles(t *testing.T) {
	started := time.Now()
	stdout, exitCode, err := runLocalCommand("sh", "-c", "(sleep 2) & printf ready")
	if err != nil {
		t.Fatalf("run local command: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if string(stdout) != "ready" {
		t.Fatalf("stdout = %q, want ready", stdout)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("command waited %s for a descendant after the direct child exited", elapsed)
	}
}
