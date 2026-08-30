package exchange

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire"
)

func TestProviderFailureLogClassificationUsesTypedFailureAuthority(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		failure   error
		wantClass string
		wantLevel slog.Level
	}{
		{"unavailable", provider.Unavailable(errors.New("network")), "unavailable", slog.LevelWarn},
		{"rejected", provider.Rejected(errors.New("rejected")), "rejected", slog.LevelWarn},
		{"invalid request", provider.InvalidRequest(errors.New("invalid")), "invalid_request", slog.LevelWarn},
		{"cancelled", provider.Cancelled(context.Canceled), "canceled", slog.LevelDebug},
		{"raw backend rejection", canonical.NewBackendError("responses", 401, "authentication_error", ""), "rejected", slog.LevelWarn},
		{"internal", provider.Internal(errors.New("invariant")), "internal", slog.LevelError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotClass, gotLevel := providerFailureLogClassification(tt.failure)
			if gotClass != tt.wantClass || gotLevel != tt.wantLevel {
				t.Fatalf("classification = (%q,%v), want (%q,%v)", gotClass, gotLevel, tt.wantClass, tt.wantLevel)
			}
		})
	}
}

func TestProviderAttemptLoggingSeparatesIngressFromTerminalCompletion(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	target := provider.TargetSnapshot{TargetID: "target-a", ProviderSpec: "gemini", Model: "model-a"}
	attempt := providerCallAttempt{target: target}
	state := exchangeState{
		input:                exchangeInput{exchangeID: "request-a"},
		providerCallAttempts: []providerCallAttempt{attempt},
	}
	call := callProviderCommand{attemptID: 1, backend: provider.Backend{Target: target}}

	logProviderAttemptCommandResult(state, call, providerIngressReceived{attemptID: 1}, time.Millisecond)
	completion, complete, _ := wire.NewResponseCompletion()
	observeProviderAttemptTerminal(state, 1, attempt, completion)
	complete(nil, nil)

	entries := decodeLogEntries(t, logs.Bytes())
	if len(entries) != 2 {
		t.Fatalf("log entries = %#v, want ingress and terminal completion", entries)
	}
	assertLogField(t, entries[0], "event", "provider_attempt_ingress_received")
	if _, ok := entries[0]["outcome"]; ok {
		t.Fatalf("handoff log unexpectedly has terminal outcome: %#v", entries[0])
	}
	assertLogField(t, entries[1], "event", "provider_attempt_finished")
	assertLogField(t, entries[1], "outcome", "completed")
}

func TestProviderAttemptLoggingRecordsSafeTerminalFailures(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	target := provider.TargetSnapshot{TargetID: "target-a", ProviderSpec: "gemini", Model: "model-a"}
	attempt := providerCallAttempt{target: target}
	state := exchangeState{input: exchangeInput{exchangeID: "request-a"}, providerCallAttempts: []providerCallAttempt{attempt}}
	call := callProviderCommand{attemptID: 1, backend: provider.Backend{Target: target}}
	failure := provider.AttemptRejectedBeforeExecution(provider.InvalidRequest(errors.New("private backend body")))
	logProviderAttemptCommandResult(state, call, providerCallFailed{attemptID: 1, failure: failure}, time.Millisecond)

	completion, _, fail := wire.NewResponseCompletion()
	observeProviderAttemptTerminal(state, 1, attempt, completion)
	fail(responseFailure("provider_stream_decode", canonical.InternalError("provider stream event is invalid")))

	entries := decodeLogEntries(t, logs.Bytes())
	if len(entries) != 2 {
		t.Fatalf("log entries = %#v, want two terminal failures", entries)
	}
	assertLogField(t, entries[0], "event", "provider_attempt_finished")
	assertLogField(t, entries[0], "outcome", "failed_before_handoff")
	assertLogField(t, entries[0], "failure_stage", "provider_transport")
	assertLogField(t, entries[1], "outcome", "aborted_after_handoff")
	assertLogField(t, entries[1], "error_code", string(canonical.ErrorCodeInternal))
	assertLogField(t, entries[1], "error_message", "provider stream event is invalid")
	assertLogField(t, entries[1], "failure_stage", "provider_stream_decode")
	if strings.Contains(logs.String(), "private backend body") {
		t.Fatalf("logs exposed backend body: %s", logs.String())
	}
}

func TestProviderAttemptLoggingDoesNotInventUnstagedFailureOwnership(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	attempt := providerCallAttempt{target: provider.TargetSnapshot{TargetID: "target-a", ProviderSpec: "gemini", Model: "model-a"}}
	state := exchangeState{input: exchangeInput{exchangeID: "request-a"}}
	completion, _, fail := wire.NewResponseCompletion()
	observeProviderAttemptTerminal(state, 1, attempt, completion)
	fail(errors.New("private terminal detail"))

	entries := decodeLogEntries(t, logs.Bytes())
	if len(entries) != 1 {
		t.Fatalf("log entries = %#v, want one terminal failure", entries)
	}
	entry := entries[0]
	assertLogField(t, entry, "failure_stage", "unknown")
	assertLogField(t, entry, "error_type", "*errors.errorString")
	for _, forbidden := range []string{"error_origin", "error_code", "error_message"} {
		if _, ok := entry[forbidden]; ok {
			t.Fatalf("unstaged error acquired %s: %#v", forbidden, entry)
		}
	}
	if strings.Contains(logs.String(), "private terminal detail") {
		t.Fatalf("logs exposed arbitrary error prose: %s", logs.String())
	}
}

func TestProviderAttemptLoggingKeepsCancellationClientOwned(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	attempt := providerCallAttempt{target: provider.TargetSnapshot{TargetID: "target-a", ProviderSpec: "gemini", Model: "model-a"}}
	state := exchangeState{input: exchangeInput{exchangeID: "request-a"}}
	completion, _, fail := wire.NewResponseCompletion()
	observeProviderAttemptTerminal(state, 1, attempt, completion)
	fail(context.Canceled)

	entries := decodeLogEntries(t, logs.Bytes())
	if len(entries) != 1 {
		t.Fatalf("log entries = %#v, want one terminal cancellation", entries)
	}
	entry := entries[0]
	assertLogField(t, entry, "outcome", "aborted_after_handoff")
	assertLogField(t, entry, "error_origin", "client")
	assertLogField(t, entry, "error_type", "context.Canceled")
	assertLogField(t, entry, "failure_stage", "unknown")
	if _, ok := entry["error_code"]; ok {
		t.Fatalf("cancellation acquired a Swobu error code: %#v", entry)
	}
}

func TestProviderAttemptLoggingNamesUnderlyingStagedErrorType(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	attempt := providerCallAttempt{target: provider.TargetSnapshot{TargetID: "target-a"}}
	completion, _, fail := wire.NewResponseCompletion()
	observeProviderAttemptTerminal(exchangeState{}, 1, attempt, completion)
	fail(responseFailure("client_stream_encode", errors.New("private encoder detail")))

	entries := decodeLogEntries(t, logs.Bytes())
	assertLogField(t, entries[0], "failure_stage", "client_stream_encode")
	assertLogField(t, entries[0], "error_type", "*errors.errorString")
	if strings.Contains(logs.String(), "private encoder detail") {
		t.Fatalf("logs exposed arbitrary encoder prose: %s", logs.String())
	}
}

func TestProviderAttemptLoggingNamesSemanticPrefetchFailureBeforeHandoff(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	attempt := providerCallAttempt{target: provider.TargetSnapshot{TargetID: "target-a", ProviderSpec: "gemini", Model: "model-a"}}
	state := exchangeState{input: exchangeInput{exchangeID: "request-a"}}
	logProviderAttemptFailedBeforeHandoff(state, 1, attempt, responseFailure("provider_stream_decode", provider.Unavailable(errors.New("private transport detail"))))

	entries := decodeLogEntries(t, logs.Bytes())
	assertLogField(t, entries[0], "outcome", "failed_before_handoff")
	assertLogField(t, entries[0], "failure_class", "unavailable")
	assertLogField(t, entries[0], "failure_stage", "provider_stream_decode")
	if strings.Contains(logs.String(), "private transport detail") {
		t.Fatalf("logs exposed arbitrary provider prose: %s", logs.String())
	}
}

func TestProviderAttemptLoggingWarnsForTypedBackendRejectionBeforeHandoff(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	attempt := providerCallAttempt{target: provider.TargetSnapshot{TargetID: "target-a", ProviderSpec: "ollama", Model: "model-a"}}
	state := exchangeState{input: exchangeInput{exchangeID: "request-a"}}
	logProviderAttemptFailedBeforeHandoff(
		state,
		1,
		attempt,
		responseFailure("provider_stream_decode", canonical.NewBackendError("responses", 401, "authentication_error", "")),
	)

	entries := decodeLogEntries(t, logs.Bytes())
	if len(entries) != 1 {
		t.Fatalf("log entries = %#v, want one provider rejection", entries)
	}
	assertLogField(t, entries[0], "level", "WARN")
	assertLogField(t, entries[0], "outcome", "failed_before_handoff")
	assertLogField(t, entries[0], "failure_class", "rejected")
	assertLogField(t, entries[0], "failure_stage", "provider_stream_decode")
	assertLogField(t, entries[0], "error_type", "canonical.BackendError")
	assertLogField(t, entries[0], "error_origin", "backend")
	assertLogField(t, entries[0], "status_code", float64(401))
	if strings.Contains(logs.String(), "authentication_error") {
		t.Fatalf("logs exposed backend prose: %s", logs.String())
	}
}

func TestProviderAttemptLoggingKeepsPreHandoffCancellationClientOwned(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	attempt := providerCallAttempt{target: provider.TargetSnapshot{TargetID: "target-a", ProviderSpec: "gemini", Model: "model-a"}}
	state := exchangeState{input: exchangeInput{exchangeID: "request-a"}}
	logProviderAttemptFailedBeforeHandoff(state, 1, attempt, context.Canceled)

	entries := decodeLogEntries(t, logs.Bytes())
	assertLogField(t, entries[0], "outcome", "failed_before_handoff")
	assertLogField(t, entries[0], "failure_class", "canceled")
	assertLogField(t, entries[0], "error_origin", "client")
	assertLogField(t, entries[0], "error_type", "context.Canceled")
}

func decodeLogEntries(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var entries []map[string]any
	for decoder.More() {
		var entry map[string]any
		if err := decoder.Decode(&entry); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func assertLogField(t *testing.T, entry map[string]any, key string, want any) {
	t.Helper()
	if got := entry[key]; got != want {
		t.Fatalf("%s = %#v, want %#v in %#v", key, got, want, entry)
	}
}
