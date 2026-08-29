package wire

import (
	"context"
	"errors"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type failingResponseStream struct{ err error }

func (s failingResponseStream) Next(context.Context) (canonical.Event, error) {
	return canonical.Event{}, s.err
}

func (failingResponseStream) Close(context.Context) error { return nil }

func TestEncodedResponseBodySettlesCompletionWithUpstreamFailureBeforeClose(t *testing.T) {
	cause := StageResponseFailure("provider_stream_decode", errors.New("invalid provider frame"))
	completion, _, fail := NewResponseCompletion()
	body := NewEncodedResponseBody(context.Background(), failingResponseStream{err: cause},
		func(canonical.Event) ([][]byte, error) { return nil, nil }, completion, fail)

	if _, err := body.Read(make([]byte, 1)); !errors.Is(err, cause) {
		t.Fatalf("read error = %v, want provider cause", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	snapshot := completion.Snapshot()
	stage, ok := ResponseFailureStage(snapshot.Err)
	if snapshot.State != CompletionFailed || !ok || stage != "provider_stream_decode" {
		t.Fatalf("completion = %#v, stage=%q, ok=%v", snapshot, stage, ok)
	}
}
