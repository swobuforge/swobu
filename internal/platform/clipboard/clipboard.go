// Package clipboard provides best-effort system clipboard access for the
// Cockpit operator surface: copy diagnostics, copy endpoint URLs, etc.
package clipboard

import (
	"fmt"
	"os"
	"sync"

	gclip "golang.design/x/clipboard"
)

var (
	initOnce   sync.Once
	initResult error
)

func initClipboard() {
	defer func() {
		if r := recover(); r != nil {
			initResult = fmt.Errorf("clipboard init panicked: %v", r)
		}
	}()
	initResult = gclip.Init()
}

func initialized() bool {
	initOnce.Do(initClipboard)
	return initResult == nil
}

// TryWriteText attempts to copy text to the system clipboard.
// On success it returns true and a nil error.
// On failure it returns false and the error (caller may fall back).
func TryWriteText(text string) (ok bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
			err = fmt.Errorf("clipboard write panicked: %v", r)
		}
	}()
	if !initialized() {
		return false, fmt.Errorf("clipboard write: init failed: %w", initResult)
	}
	ch := gclip.Write(gclip.FmtText, []byte(text))
	if ch == nil {
		return false, fmt.Errorf("clipboard write: write returned nil channel")
	}
	return true, nil
}

// WriteTempFileFallback writes text to a temporary file and returns its path.
// Use when the system clipboard is unavailable and the operator needs a
// fallback paste location.
func WriteTempFileFallback(dir, prefix, text string) (string, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("clipboard fallback: mkdir %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, prefix+"*.txt")
	if err != nil {
		return "", fmt.Errorf("clipboard fallback: create temp: %w", err)
	}
	path := f.Name()
	if _, werr := f.WriteString(text); werr != nil {
		f.Close()
		os.Remove(path)
		return "", fmt.Errorf("clipboard fallback: write temp: %w", werr)
	}
	if cerr := f.Close(); cerr != nil {
		os.Remove(path)
		return "", fmt.Errorf("clipboard fallback: close temp: %w", cerr)
	}
	return path, nil
}
