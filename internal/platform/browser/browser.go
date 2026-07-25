// Package browser opens URLs in the system default browser.
package browser

import (
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"

	clibrowser "github.com/cli/browser"
)

var openMu sync.Mutex

// Open starts the default browser for the given raw URL.
// It accepts only absolute HTTP(S) URLs and returns platform opener failures.
// Child-process output is discarded so a failed platform opener cannot paint
// diagnostics over a terminal UI. The mutex protects cli/browser's writer
// globals while they are temporarily redirected.
func Open(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("browser open: URL is missing")
	}
	target, err := url.Parse(raw)
	if err != nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") {
		return fmt.Errorf("browser open: URL must be absolute HTTP(S)")
	}

	openMu.Lock()
	previousStdout, previousStderr := clibrowser.Stdout, clibrowser.Stderr
	clibrowser.Stdout, clibrowser.Stderr = io.Discard, io.Discard
	defer func() {
		clibrowser.Stdout, clibrowser.Stderr = previousStdout, previousStderr
		openMu.Unlock()
	}()

	if err := clibrowser.OpenURL(target.String()); err != nil {
		return fmt.Errorf("browser open: %w", err)
	}
	return nil
}
