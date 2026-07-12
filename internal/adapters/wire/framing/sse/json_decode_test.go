package sse

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

func TestDecodePermissiveJSON_AcceptsUnexpectedFieldAndLogsPointer(t *testing.T) {
	var dto struct {
		Model string `json:"model"`
	}
	capture := &captureSlogHandler{}
	logger := slog.New(capture)

	err := DecodePermissiveJSON(json.RawMessage(`{"model":"m","a/b~c":true}`), &dto, "messages request", logger)
	if err != nil {
		t.Fatalf("DecodePermissiveJSON() error = %v", err)
	}
	if dto.Model != "m" {
		t.Fatalf("model = %q, want %q", dto.Model, "m")
	}
	records := capture.Records()
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}
	record := records[0]
	if record.Level != slog.LevelWarn {
		t.Fatalf("level = %s, want %s", record.Level, slog.LevelWarn)
	}
	if record.Message != "unexpected request field" {
		t.Fatalf("message = %q, want %q", record.Message, "unexpected request field")
	}
	attrs := recordAttrs(record)
	if got := attrs["surface"]; got != "messages request" {
		t.Fatalf("surface = %v, want %q", got, "messages request")
	}
	if got := attrs["json_field"]; got != "a/b~c" {
		t.Fatalf("json_field = %v, want %q", got, "a/b~c")
	}
	if got := attrs["json_pointer"]; got != "/a~1b~0c" {
		t.Fatalf("json_pointer = %v, want %q", got, "/a~1b~0c")
	}
}

func TestDecodePermissiveJSON_RejectsMalformedJSON(t *testing.T) {
	var dto struct {
		Model string `json:"model"`
	}

	err := DecodePermissiveJSON(json.RawMessage(`{"model":`), &dto, "messages request", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
}

func TestDecodePermissiveJSON_RejectsTrailingData(t *testing.T) {
	var dto struct {
		Model string `json:"model"`
	}

	err := DecodePermissiveJSON(json.RawMessage(`{"model":"m"} garbage`), &dto, "messages request", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var compatErr canonical.Error
	if !errors.As(err, &compatErr) {
		t.Fatalf("expected canonical.Error, got %T", err)
	}
	if compatErr.Code != canonical.ErrorCodeBadRequest {
		t.Fatalf("code = %q, want %q", compatErr.Code, canonical.ErrorCodeBadRequest)
	}
}

type captureSlogHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureSlogHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *captureSlogHandler) Handle(_ context.Context, r slog.Record) error {
	record := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(attr slog.Attr) bool {
		record.AddAttrs(attr)
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, record)
	h.mu.Unlock()
	return nil
}

func (h *captureSlogHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *captureSlogHandler) WithGroup(_ string) slog.Handler {
	return h
}

func (h *captureSlogHandler) Records() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	copy(out, h.records)
	return out
}

func recordAttrs(r slog.Record) map[string]any {
	attrs := make(map[string]any)
	r.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})
	return attrs
}
