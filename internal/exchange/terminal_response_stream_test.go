package exchange

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/wire"
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

func TestTerminalResponseStreamHidesUnderlyingPostStartFailure(t *testing.T) {
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
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("operator log missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, underlying.Error()) {
		t.Fatalf("stable WARN log exposed arbitrary cause: %s", got)
	}
}

func TestTerminalResponseStreamLogsOnlyStructuredPostStartFailureDiagnostics(t *testing.T) {
	underlying := errors.New("item.completed ordinal 2 is duplicated")
	upstream := &failAfterEventsStream{
		events: []canonical.Event{{
			ExchangeID: "exchange_1",
			Seq:        1,
			Kind:       canonical.EventEnvelopeStart,
			EnvID:      "response_1",
			Payload:    canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse},
		}},
		err: wire.StageResponseFailure("canonical_response_validation", underlying),
	}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	stream := newTerminalResponseStream(upstream)
	if _, err := stream.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Next(context.Background()); err != nil {
		t.Fatal(err)
	}

	entries := decodeLogEntries(t, logs.Bytes())
	if len(entries) != 2 {
		t.Fatalf("log entries = %#v, want stable warning and private diagnostic", entries)
	}
	var stable, diagnostic map[string]any
	for _, entry := range entries {
		if entry["event"] == "provider_stream_failure_diagnostic" {
			diagnostic = entry
		} else {
			stable = entry
		}
	}
	assertLogField(t, stable, "event", "provider_stream_failed_after_start")
	if strings.Contains(fmt.Sprint(stable), underlying.Error()) {
		t.Fatalf("stable warning exposed private diagnostic: %#v", stable)
	}
	assertLogField(t, diagnostic, "exchange_id", "exchange_1")
	assertLogField(t, diagnostic, "failure_stage", "canonical_response_validation")
	if _, exists := diagnostic["diagnostic_error"]; exists || strings.Contains(logs.String(), underlying.Error()) {
		t.Fatalf("diagnostic exposed arbitrary error prose: %#v", diagnostic)
	}
}

func TestTerminalResponseStreamPreservesStructuredProviderFailure(t *testing.T) {
	upstream := &failAfterEventsStream{
		events: []canonical.Event{{ExchangeID: "exchange_1", Seq: 1, Kind: canonical.EventEnvelopeStart, EnvID: "response_1", Payload: canonical.EnvelopeStartPayload{Kind: canonical.EnvResponse}}},
		err:    canonical.NewStructuredBackendError("target-a", protocolkind.Responses, 429, canonical.BackendErrorDetail{Code: "quota", Type: "rate_limit_error", Message: "limit reached"}, ""),
	}
	stream := newTerminalResponseStream(upstream)
	_, _ = stream.Next(context.Background())
	event, err := stream.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	payload, ok := event.Payload.(canonical.ErrorPayload)
	if !ok || payload.Code != "quota" || payload.Message != "limit reached" {
		t.Fatalf("terminal payload = %#v", event.Payload)
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
