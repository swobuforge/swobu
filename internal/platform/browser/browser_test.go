package browser

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	clibrowser "github.com/cli/browser"
)

func TestOpen_EmptyURLReturnsError(t *testing.T) {
	err := Open("")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if got := err.Error(); got != "browser open: URL is missing" {
		t.Fatalf("error = %q, want URL is missing", got)
	}
}

func TestOpen_WhitespaceOnlyURLReturnsError(t *testing.T) {
	err := Open("  \t")
	if err == nil {
		t.Fatal("expected error for whitespace-only URL")
	}
}

func TestOpen_RejectsUnsupportedScheme(t *testing.T) {
	err := Open("file:///tmp/login.html")
	if err == nil {
		t.Fatal("expected non-HTTP URL to fail")
	}
	if got := err.Error(); got != "browser open: URL must be absolute HTTP(S)" {
		t.Fatalf("error = %q, want unsupported URL error", got)
	}
}

func TestOpen_PropagatesPlatformOpenerFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fake xdg-open reproduction is Linux-specific")
	}

	binDir := t.TempDir()
	opener := filepath.Join(binDir, "xdg-open")
	if err := os.WriteFile(opener, []byte("#!/bin/sh\nprintf 'opener stdout noise\\n'\nprintf 'opener stderr noise\\n' >&2\nexit 23\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	var stdout, stderr bytes.Buffer
	previousStdout, previousStderr := clibrowser.Stdout, clibrowser.Stderr
	clibrowser.Stdout, clibrowser.Stderr = &stdout, &stderr
	defer func() {
		clibrowser.Stdout, clibrowser.Stderr = previousStdout, previousStderr
	}()

	err := Open("https://example.test/login")
	if err == nil {
		t.Fatal("expected non-zero platform opener exit to be returned")
	}
	if !strings.Contains(err.Error(), "exit status 23") {
		t.Fatalf("error = %q, want platform opener exit status", err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("platform opener output escaped: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if clibrowser.Stdout != &stdout || clibrowser.Stderr != &stderr {
		t.Fatal("browser output writers were not restored after the open attempt")
	}
}
