package adapters

import (
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/cockpit/ui"
	"github.com/swobuforge/swobu/internal/platform/clipboard"
)

func TestCockpitClipboardResultPreservesAvailabilityDistinction(t *testing.T) {
	if got := cockpitClipboardResult(clipboard.WriteResult{Status: clipboard.WriteUnavailable, Err: errors.New("no display")}); got.Status != ui.CopyUnavailable || got.Err != nil {
		t.Fatalf("unavailable result = %#v", got)
	}

	transportErr := errors.New("write panicked")
	if got := cockpitClipboardResult(clipboard.WriteResult{Status: clipboard.WriteFailed, Err: transportErr}); got.Status != ui.CopyFailed || !errors.Is(got.Err, transportErr) {
		t.Fatalf("failed result = %#v", got)
	}

	if got := cockpitClipboardResult(clipboard.WriteResult{Status: clipboard.WriteOK}); got.Status != ui.CopyOK || got.Err != nil {
		t.Fatalf("successful result = %#v", got)
	}
}
