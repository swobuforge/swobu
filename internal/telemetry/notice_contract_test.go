package telemetry

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStore_EnsureNoticeShownWithDisclosure_PrintsOnceAndPersists(t *testing.T) {
	t.Parallel()

	statePath := filepath.Join(t.TempDir(), "telemetry", "state.json")
	store := Store{
		StatePath: statePath,
		Now:       func() time.Time { return time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC) },
		Rand:      bytes.NewReader([]byte{0x01, 0x02, 0x03, 0x04}),
	}

	var out bytes.Buffer
	state, err := store.EnsureNoticeShownWithDisclosure(&out)
	if err != nil {
		t.Fatalf("EnsureNoticeShownWithDisclosure returned error: %v", err)
	}
	if !state.NoticeShown {
		t.Fatal("notice_shown = false, want true")
	}
	if got := strings.TrimSpace(out.String()); got != strings.TrimSpace(FirstRunNoticeText()) {
		t.Fatalf("disclosure output mismatch:\n%s", got)
	}

	out.Reset()
	second, err := store.EnsureNoticeShownWithDisclosure(&out)
	if err != nil {
		t.Fatalf("second EnsureNoticeShownWithDisclosure returned error: %v", err)
	}
	if !second.NoticeShown {
		t.Fatal("second notice_shown = false, want true")
	}
	if out.String() != "" {
		t.Fatalf("second disclosure output = %q, want empty", out.String())
	}
}
