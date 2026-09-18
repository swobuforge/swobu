package wire

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type failingResponseStream struct{ err error }

func (s failingResponseStream) Next(context.Context) (canonical.Event, error) {
	return canonical.Event{}, s.err
}

func (failingResponseStream) Close(context.Context) error { return nil }

type terminalFailureResponseStream struct {
	canonical.ResponseStream
	cause error
}

func (s terminalFailureResponseStream) TerminalFailureCause() error { return s.cause }

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

func TestEncodedResponseBodyPreservesUnexpectedTerminalEncoderFailure(t *testing.T) {
	events := canonical.NewSliceEventReader([]canonical.Event{{
		Kind:    canonical.EventError,
		Payload: canonical.ErrorPayload{Code: "provider_stream_decode_failed", Message: "provider stream failed after response start"},
	}})
	completion, _, fail := NewResponseCompletion()
	body := NewEncodedResponseBody(context.Background(), events,
		func(canonical.Event) ([][]byte, error) { return nil, errors.New("client cannot encode terminal error") }, completion, fail)

	if _, err := body.Read(make([]byte, 1)); err == nil || !strings.Contains(err.Error(), "client cannot encode terminal error") {
		t.Fatalf("read error = %v, want encoder failure", err)
	}
	snapshot := completion.Snapshot()
	stage, ok := ResponseFailureStage(snapshot.Err)
	if snapshot.State != CompletionFailed || !ok || stage != "client_stream_encode" {
		t.Fatalf("completion = %#v, stage=%q, ok=%v", snapshot, stage, ok)
	}
}

func TestEncodedResponseBodyRestoresProviderCauseOnlyForUnrepresentableTerminalProjection(t *testing.T) {
	cause := StageResponseFailure("provider_stream_decode", errors.New("private provider detail"))
	events := terminalFailureResponseStream{
		ResponseStream: canonical.NewSliceEventReader([]canonical.Event{{
			Kind: canonical.EventError,
			Payload: canonical.ErrorPayload{
				Code: "provider_stream_decode_failed", Message: "provider stream failed after response start",
			},
		}}),
		cause: cause,
	}
	completion, _, fail := NewResponseCompletion()
	body := NewEncodedResponseBody(context.Background(), events,
		func(canonical.Event) ([][]byte, error) {
			return nil, TerminalProjectionUnrepresentable(errors.New("no in-band terminal form"))
		}, completion, fail)

	if _, err := body.Read(make([]byte, 1)); !errors.Is(err, cause) {
		t.Fatalf("read error = %v, want provider cause", err)
	}
	stage, ok := ResponseFailureStage(completion.Snapshot().Err)
	if !ok || stage != "provider_stream_decode" {
		t.Fatalf("completion stage = %q, ok=%v", stage, ok)
	}
}
