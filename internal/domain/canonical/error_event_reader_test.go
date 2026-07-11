package canonical

import (
	"context"
	"errors"
	"testing"
)

func TestErrorEventReader_NextReturnsConfiguredError(t *testing.T) {
	want := errors.New("boom")
	reader := NewErrorEventReader(want)
	if _, err := reader.Next(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Next error=%v want %v", err, want)
	}
}
