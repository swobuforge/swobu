package clipboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteTempFileFallback_CreatesFileWithContent(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteTempFileFallback(dir, "swobu-test", "diagnostics data")
	if err != nil {
		t.Fatalf("WriteTempFileFallback error: %v", err)
	}
	if !strings.HasPrefix(path, dir) {
		t.Fatalf("path %q not under dir %q", path, dir)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(content) != "diagnostics data" {
		t.Fatalf("content = %q, want diagnostics data", string(content))
	}
}

func TestWriteTempFileFallback_WritesToTempDirWhenDirEmpty(t *testing.T) {
	path, err := WriteTempFileFallback("", "swobu-test", "fallback text")
	if err != nil {
		t.Fatalf("WriteTempFileFallback error: %v", err)
	}
	defer os.Remove(path)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(content) != "fallback text" {
		t.Fatalf("content = %q, want fallback text", string(content))
	}
}

func TestWriteTempFileFallback_CreatesDirIfMissing(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "nested", "swobu")
	path, err := WriteTempFileFallback(dir, "swobu-test", "nested data")
	if err != nil {
		t.Fatalf("WriteTempFileFallback error: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(content) != "nested data" {
		t.Fatalf("content = %q, want nested data", string(content))
	}
}

func TestTryWriteText_CompletesPromptly(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = TryWriteText("swobu-test-clipboard-content")
	}()

	select {
	case <-done:
		// success: completed without hanging
	case <-time.After(1 * time.Second):
		t.Fatal("TryWriteText hung / did not return promptly")
	}
}
