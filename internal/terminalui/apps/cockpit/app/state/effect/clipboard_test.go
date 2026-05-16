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

func TestNormalizeAuthSessionCopyValue_StripsEmbeddedWhitespace(t *testing.T) {
	t.Parallel()
	in := " https://auth.openai.com/oauth/authorize?client_id=abc \n&code_challenge=xyz\t&scope=openid+email "
	got := normalizeAuthSessionCopyValue(in)
	if strings.ContainsAny(got, " \n\t\r") {
		t.Fatalf("normalized value still contains whitespace: %q", got)
	}
	if !strings.Contains(got, "client_id=abc&code_challenge=xyz&scope=openid+email") {
		t.Fatalf("normalized value missing expected query shape: %q", got)
	}
}
