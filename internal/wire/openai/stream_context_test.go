package openai

import (
	"context"
	"io"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type streamContextKey struct{}

type contextRecordingResponseStream struct {
	want       any
	nextCalls  int
	closeCalls int
}

func (s *contextRecordingResponseStream) Next(ctx context.Context) (canonical.Event, error) {
	if got := ctx.Value(streamContextKey{}); got != s.want {
		return canonical.Event{}, io.ErrUnexpectedEOF
	}
	s.nextCalls++
	return canonical.Event{}, io.EOF
}

func (s *contextRecordingResponseStream) Close(ctx context.Context) error {
	if got := ctx.Value(streamContextKey{}); got != s.want {
		return io.ErrUnexpectedEOF
	}
	s.closeCalls++
	return nil
}

type unusedEnvelopeEncoder struct{}

func (unusedEnvelopeEncoder) EncodeEnvelopeEvent(canonical.Event) ([][]byte, error) { return nil, nil }
func (unusedEnvelopeEncoder) Finish() ([][]byte, error)                             { return nil, nil }

func TestEncodeEnvelopeStreamUsesInvocationContextAndClosesExactlyOnce(t *testing.T) {
	want := &struct{}{}
	ctx := context.WithValue(context.Background(), streamContextKey{}, want)
	events := &contextRecordingResponseStream{want: want}

	result, err := EncodeEnvelopeStream(ctx, events, unusedEnvelopeEncoder{})
	if err != nil {
		t.Fatalf("EncodeEnvelopeStream: %v", err)
	}
	if _, err := io.ReadAll(result.Stream.Body); err != nil {
		t.Fatalf("read encoded stream: %v", err)
	}
	if events.closeCalls != 0 {
		t.Fatalf("stream closed before delivery owner called Close: %d", events.closeCalls)
	}
	if err := result.Stream.Body.Close(); err != nil {
		t.Fatalf("close encoded stream: %v", err)
	}
	if events.nextCalls != 1 || events.closeCalls != 1 {
		t.Fatalf("stream calls = next:%d close:%d, want 1/1", events.nextCalls, events.closeCalls)
	}
}
