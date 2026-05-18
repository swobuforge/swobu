package effect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	cliplib "golang.design/x/clipboard"
)

var (
	clipboardInit         = cliplib.Init
	writeClipboardBytes   = func(text string) error { cliplib.Write(cliplib.FmtText, []byte(text)); return nil }
	writeCopyFallbackFile = writeCopyFallbackFileOnDisk
	clipboardOnce         sync.Once
	clipboardInitErr      error
)

func copyValueNote(text string) string {
	text = strings.TrimSpace(text) // swobu:io-string source=boundary
	if text == "" {
		return "nothing to copy"
	}
	fallbackPath, fallbackErr := writeCopyFallbackFile(text)
	if err := writeClipboardText(text); err == nil {
		return "copied"
	}
	if fallbackErr == nil {
		return "clipboard unavailable; saved copy at " + fallbackPath
	}
	return "clipboard unavailable; value stays visible in row"
}

func writeClipboardText(text string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("clipboard unavailable: %v", recovered)
		}
	}()
	clipboardOnce.Do(func() {
		clipboardInitErr = clipboardInit()
	})
	if clipboardInitErr != nil {
		return clipboardInitErr
	}
	return writeClipboardBytes(text)
}

func writeCopyFallbackFileOnDisk(text string) (string, error) {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("swobu-copy-%d.txt", os.Getpid()))
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
