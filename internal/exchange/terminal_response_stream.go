package exchange

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// terminalResponseStream converts a provider read failure after response start
// into one coherent canonical terminal error. It owns delivery truth only; it
// knows nothing about checkpoints, fingerprints, or storage.
type terminalResponseStream struct {
	upstream   canonical.ResponseStream
	started    bool
	terminated bool
	pending    []canonical.Event
	last       canonical.Event
}

func newTerminalResponseStream(upstream canonical.ResponseStream) canonical.ResponseStream {
	return &terminalResponseStream{upstream: upstream}
}

func (s *terminalResponseStream) Next(ctx context.Context) (canonical.Event, error) {
	if len(s.pending) > 0 {
		event := s.pending[0]
		s.pending = s.pending[1:]
		return event, nil
	}
	if s.terminated {
		return canonical.Event{}, io.EOF
	}
	event, err := s.upstream.Next(ctx)
	if err != nil {
		if s.started && (errors.Is(err, io.EOF) || !errors.Is(err, context.Canceled)) {
			code, message := "provider_stream_decode_failed", "provider stream failed after response start"
			if errors.Is(err, io.EOF) {
				code, message = "provider_stream_incomplete", "provider stream ended before completed"
			}
			s.pending = terminalFailureEvents(s.last, code, message)
			s.terminated = true
			return s.Next(ctx)
		}
		return canonical.Event{}, err
	}
	if event.Kind == canonical.EventEnvelopeStart {
		if payload, ok := event.Payload.(canonical.EnvelopeStartPayload); ok && payload.Kind == canonical.EnvResponse {
			s.started = true
			event.Meta.NativeID = ""
		}
	}
	s.last = event
	if _, terminal := responseTerminalStatus(event); terminal {
		s.terminated = true
	}
	return event, nil
}

func (s *terminalResponseStream) Close(ctx context.Context) error {
	if s.upstream == nil {
		return nil
	}
	return s.upstream.Close(ctx)
}

func terminalFailureEvents(base canonical.Event, code, message string) []canonical.Event {
	when := base.Time
	if when.IsZero() {
		when = time.Now().UTC()
	}
	return []canonical.Event{
		{
			ExchangeID: base.ExchangeID, Seq: base.Seq + 1, Time: when,
			Kind: canonical.EventError, EnvID: base.EnvID, ParentID: base.ParentID,
			Payload: canonical.ErrorPayload{Code: code, Message: message},
		},
		{
			ExchangeID: base.ExchangeID, Seq: base.Seq + 2, Time: time.Now().UTC(),
			Kind: canonical.EventEnvelopeEnd, EnvID: base.EnvID, ParentID: base.ParentID,
			Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusError},
		},
	}
}

func responseTerminalStatus(event canonical.Event) (canonical.EnvelopeStatus, bool) {
	if event.Kind != canonical.EventEnvelopeEnd {
		return "", false
	}
	payload, ok := event.Payload.(canonical.EnvelopeEndPayload)
	if !ok || payload.Kind != canonical.EnvResponse {
		return "", false
	}
	return payload.Status, true
}
