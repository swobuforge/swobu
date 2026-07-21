package exchange

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

type failAfterEventsStream struct {
	events []canonical.Event
	err    error
}

func (s *failAfterEventsStream) Next(context.Context) (canonical.Event, error) {
	if len(s.events) == 0 {
		return canonical.Event{}, s.err
	}
	event := s.events[0]
	s.events = s.events[1:]
	return event, nil
}

func (s *failAfterEventsStream) Close(context.Context) error { return nil }

func TestTerminalResponseStreamLogsUnderlyingPostStartFailure(t *testing.T) {
	underlying := errors.New("item.completed ordinal 2 is duplicated")
	upstream := &failAfterEventsStream{
		events: []canonical.Event{{
			ExchangeID: "exchange_1",
			Seq:        1,
			Kind:       canonical.EventEnvelopeStart,
			EnvID:      "response_1",
			Payload:    canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse},
		}},
		err: underlying,
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	stream := newTerminalResponseStream(upstream)
	if _, err := stream.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	event, err := stream.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := event.Payload.(canonical.ErrorPayload)
	if !ok || payload.Code != "provider_stream_decode_failed" || strings.Contains(payload.Message, underlying.Error()) {
		t.Fatalf("client terminal error = %#v", event.Payload)
	}
	got := logs.String()
	for _, want := range []string{
		"event=provider_stream_failed_after_start",
		"exchange_id=exchange_1",
		"code=provider_stream_decode_failed",
		`error="item.completed ordinal 2 is duplicated"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("operator log missing %q: %s", want, got)
		}
	}
}

func TestTerminalResponseStreamDoesNotWarnForCleanCompletion(t *testing.T) {
	upstream := &failAfterEventsStream{
		events: []canonical.Event{{
			Kind:    canonical.EventEnvelopeEnd,
			Payload: canonical.EnvelopeEndPayload{Kind: canonical.EnvResponse, Status: canonical.EnvelopeStatusCompleted},
		}},
		err: io.EOF,
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	stream := newTerminalResponseStream(upstream)
	if _, err := stream.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("terminal read error = %v, want EOF", err)
	}
	if logs.Len() != 0 {
		t.Fatalf("clean completion log = %q", logs.String())
	}
}
