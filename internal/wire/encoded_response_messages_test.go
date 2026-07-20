package wire

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestEncodedResponseMessagesPreservesEveryEncodedMessageBoundary(t *testing.T) {
	large := bytes.Repeat([]byte("x"), 5000)
	events := canonical.NewSliceEventReader([]canonical.Event{{Kind: canonical.EventTextDelta}})
	completion, _, fail := NewResponseCompletion()
	stream := NewEncodedResponseMessages(events, func(canonical.Event) ([][]byte, error) {
		return [][]byte{[]byte(`{"type":"first"}`), large}, nil
	}, completion, fail)

	first, err := stream.Next(context.Background())
	if err != nil || string(first) != `{"type":"first"}` {
		t.Fatalf("first = %q, err=%v", first, err)
	}
	second, err := stream.Next(context.Background())
	if err != nil || !bytes.Equal(second, large) {
		t.Fatalf("second bytes = %d, err=%v", len(second), err)
	}
	if _, err := stream.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal error = %v, want EOF", err)
	}
}
