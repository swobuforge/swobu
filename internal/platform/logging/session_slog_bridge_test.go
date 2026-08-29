package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestBufferedHandler_BuffersAndFlushes(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	base := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo}))
	handler := NewBufferedHandler(base.Handler())
	logger := slog.New(handler)

	logger.Info("before", "component", "test", "event", "before")
	if out.Len() != 0 {
		t.Fatalf("buffered record should not be written before flush; got=%q", out.String())
	}
	handler.Flush(context.Background())

	text := out.String()
	if !strings.Contains(text, "before") {
		t.Fatalf("expected transcript record present; got=%q", text)
	}
}

func TestBufferedHandler_WithAttrsAndGroups_PreservedAcrossFlush(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	base := slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelInfo}))
	handler := NewBufferedHandler(base.Handler())
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	record.AddAttrs(slog.String("event", "probe"))

	scoped := handler.WithGroup("outer").WithAttrs([]slog.Attr{slog.String("component", "bridge")})
	if err := scoped.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle in interactive mode: %v", err)
	}
	handler.Flush(context.Background())

	text := out.String()
	if !strings.Contains(text, "outer.component=bridge") {
		t.Fatalf("expected grouped attribute after flush; got=%q", text)
	}
	if !strings.Contains(text, "outer.event=probe") {
		t.Fatalf("expected record attribute after flush; got=%q", text)
	}
}
