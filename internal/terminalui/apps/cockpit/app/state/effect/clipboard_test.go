package effect

import (
	"strings"
	"sync"
	"testing"
)

func TestCopyValueNote_ClipboardPanicFallsBack(t *testing.T) {
	origInit := clipboardInit
	origWrite := writeClipboardBytes
	origFallback := writeCopyFallbackFile
	origInitErr := clipboardInitErr
	t.Cleanup(func() {
		clipboardInit = origInit
		writeClipboardBytes = origWrite
		writeCopyFallbackFile = origFallback
		clipboardOnce = sync.Once{}
		clipboardInitErr = origInitErr
	})

	clipboardOnce = sync.Once{}
	clipboardInitErr = nil
	clipboardInit = func() error {
		panic("clipboard backend unavailable")
	}
	writeCopyFallbackFile = func(text string) (string, error) {
		return "/tmp/swobu-copy-test.txt", nil
	}

	message := copyValueNote("https://example.test")
	if !strings.Contains(message, "clipboard unavailable; saved copy at /tmp/swobu-copy-test.txt") {
		t.Fatalf("copyValueNote message=%q", message)
	}
}
