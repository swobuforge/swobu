package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunner_VersionFlag_PrintsVersion(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"--version"})
	if exitCode != ExitHealthy {
		t.Fatalf("exit code = %d, want %d; stderr=%q", exitCode, ExitHealthy, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatal("stdout empty, want version value")
	}
}

func TestRunner_VersionSubcommand_PrintsVersion(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner := Runner{Stdout: &stdout, Stderr: &stderr}

	exitCode := runner.Run(context.Background(), []string{"version"})
	if exitCode != ExitHealthy {
		t.Fatalf("exit code = %d, want %d; stderr=%q", exitCode, ExitHealthy, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatal("stdout empty, want version value")
	}
}
