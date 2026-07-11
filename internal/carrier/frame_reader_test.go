package carrier

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestFrameReaderFromReadCloser_EmitsSequencedPayloadFrames(t *testing.T) {
	reader := FrameReaderFromReadCloser(io.NopCloser(strings.NewReader("abcdef")))
	if reader == nil {
		t.Fatalf("reader = nil")
	}
	defer func() { _ = reader.Close() }()

	first, err := reader.Next(context.Background())
	if err != nil {
		t.Fatalf("first frame error: %v", err)
	}
	if first.Kind != FramePayload {
		t.Fatalf("first kind = %q, want %q", first.Kind, FramePayload)
	}
	if first.Seq != 0 {
		t.Fatalf("first seq = %d, want 0", first.Seq)
	}
	if len(first.Data) == 0 {
		t.Fatalf("first data empty")
	}

	for {
		next, err := reader.Next(context.Background())
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("next frame error: %v", err)
		}
		if next.Kind != FramePayload {
			t.Fatalf("next kind = %q, want %q", next.Kind, FramePayload)
		}
		if next.Seq <= first.Seq {
			t.Fatalf("seq not increasing: prev=%d next=%d", first.Seq, next.Seq)
		}
		first = next
	}
}
