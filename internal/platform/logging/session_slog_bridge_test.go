package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	tuisession "github.com/swobuforge/swobu/internal/terminalui/session"
)

func TestSessionBufferedHandler_BuffersInteractiveAndFlushesOnTranscript(t *testing.T) {
	t.Parallel()

	s := tuisession.New(tuisession.ModeTranscript)
	var out bytes.Buffer
	base := slog.New(NewCommonLineHandler(&out, slog.LevelInfo))
	handler := NewSessionBufferedHandler(base.Handler(), s)
	logger := slog.New(handler)

	logger.Info("before", "component", "test", "event", "before")
	if err := s.SetMode(tuisession.ModeInteractive); err != nil {
		t.Fatalf("set interactive mode: %v", err)
	}
	logger.Info("hidden", "component", "test", "event", "hidden")
	if strings.Contains(out.String(), "hidden") {
		t.Fatalf("interactive record must not be written before flush; got=%q", out.String())
	}
	if err := s.SetMode(tuisession.ModeTranscript); err != nil {
		t.Fatalf("set transcript mode: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "before") {
		t.Fatalf("expected transcript record present; got=%q", text)
	}
	if !strings.Contains(text, "hidden") {
		t.Fatalf("expected buffered record flushed on transcript mode; got=%q", text)
	}
}

func TestSessionBufferedHandler_WithAttrsAndGroups_PreservedAcrossFlush(t *testing.T) {
	t.Parallel()

	s := tuisession.New(tuisession.ModeInteractive)
	var out bytes.Buffer
	base := slog.New(NewCommonLineHandler(&out, slog.LevelInfo))
	handler := NewSessionBufferedHandler(base.Handler(), s)
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
	record.AddAttrs(slog.String("event", "probe"))

	scoped := handler.WithGroup("outer").WithAttrs([]slog.Attr{slog.String("component", "bridge")})
	if err := scoped.Handle(context.Background(), record); err != nil {
		t.Fatalf("handle in interactive mode: %v", err)
	}
	if err := s.SetMode(tuisession.ModeTranscript); err != nil {
		t.Fatalf("set transcript mode: %v", err)
	}

	text := out.String()
	if !strings.Contains(text, "outer.component=bridge") {
		t.Fatalf("expected grouped attribute after flush; got=%q", text)
	}
	if !strings.Contains(text, "outer.event=probe") {
		t.Fatalf("expected record attribute after flush; got=%q", text)
	}
}
