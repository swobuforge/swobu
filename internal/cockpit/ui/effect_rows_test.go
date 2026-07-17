package ui

import (
	"errors"
	"testing"
)

func TestRegisterEffectHooksCleanupRestoresPreviousHooks(t *testing.T) {
	firstErr := errors.New("first")
	secondErr := errors.New("second")
	cleanupFirst := RegisterEffectHooks(
		func(string) error { return firstErr },
		func(string) (bool, error) { return true, nil },
		nil,
	)
	defer cleanupFirst()

	if err := OpenURL("https://example.test"); !errors.Is(err, firstErr) {
		t.Fatalf("OpenURL first err = %v, want first hook", err)
	}

	cleanupSecond := RegisterEffectHooks(
		func(string) error { return secondErr },
		func(string) (bool, error) { return false, nil },
		func(dir, prefix, text string) (string, error) { return "/tmp/fallback.txt", nil },
	)

	if err := OpenURL("https://example.test"); !errors.Is(err, secondErr) {
		t.Fatalf("OpenURL second err = %v, want second hook", err)
	}
	if got := CopyToClipboard("payload"); got.Status != CopySavedFile || got.Path != "/tmp/fallback.txt" {
		t.Fatalf("CopyToClipboard second result = %+v, want fallback file", got)
	}

	cleanupSecond()

	if err := OpenURL("https://example.test"); !errors.Is(err, firstErr) {
		t.Fatalf("OpenURL after cleanup err = %v, want restored first hook", err)
	}
	if got := CopyToClipboard("payload"); got.Status != CopyOK {
		t.Fatalf("CopyToClipboard after cleanup = %+v, want restored first hook", got)
	}
}
