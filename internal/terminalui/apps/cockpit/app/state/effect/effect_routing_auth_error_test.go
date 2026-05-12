package effect

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeAuthSessionSurfaceError_HTMLChallengePageCollapsed(t *testing.T) {
	t.Parallel()

	err := errors.New("operator client: chatgpt login start failed: device auth start returned status 403: <!DOCTYPE html><html lang=\"en-US\"><head><title>Just a moment...</title></head></html> (code=INVALID_ARGUMENT)")
	got := normalizeAuthSessionSurfaceError(err)

	if strings.Contains(strings.ToLower(got), "<!doctype html") || strings.Contains(strings.ToLower(got), "<html") {
		t.Fatalf("normalized error leaked html: %q", got)
	}
	if !strings.Contains(got, "status 403") {
		t.Fatalf("normalized error=%q want status", got)
	}
	if !strings.Contains(got, "code=INVALID_ARGUMENT") {
		t.Fatalf("normalized error=%q want code", got)
	}
}

func TestSanitizeAuthSessionErrorMessage_TruncatesLongPlaintext(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 300)
	got := sanitizeAuthSessionErrorMessage(long)
	if len(got) >= len(long) {
		t.Fatalf("expected truncation, got len=%d", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", got)
	}
}
