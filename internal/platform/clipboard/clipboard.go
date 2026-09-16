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

// WriteStatus distinguishes expected environmental absence from an anomalous
// clipboard write failure without interpreting backend error text.
type WriteStatus uint8

const (
	WriteOK WriteStatus = iota
	WriteUnavailable
	WriteFailed
)

// WriteResult reports the typed outcome of a clipboard transfer.
type WriteResult struct {
	Status WriteStatus
	Err    error
}

// TryWriteText attempts to copy text to the system clipboard. Initialization
// failure means the environment has no usable clipboard; panics and impossible
// write results remain failures.
func TryWriteText(text string) (result WriteResult) {
	defer func() {
		if r := recover(); r != nil {
			result = WriteResult{Status: WriteFailed, Err: fmt.Errorf("clipboard write panicked: %v", r)}
		}
	}()
	if !initialized() {
		return WriteResult{Status: WriteUnavailable, Err: initResult}
	}
	ch := gclip.Write(gclip.FmtText, []byte(text))
	if ch == nil {
		return WriteResult{Status: WriteFailed, Err: fmt.Errorf("clipboard write: write returned nil channel")}
	}
	return WriteResult{Status: WriteOK}
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
