package logging

import (
	"bytes"
	"context"
	"log/slog"
	"sync"

	tuisession "github.com/swobuforge/swobu/internal/terminalui/session"
)

type sessionBufferedSurface interface {
	Mode() tuisession.Mode
	OnModeChange(func(prev, next tuisession.Mode))
}

// NewSessionBufferedHandler returns a slog handler policy that buffers records
// while interactive mode is active and flushes records when transcript mode resumes.
func NewSessionBufferedHandler(base slog.Handler, uiMode sessionBufferedSurface) slog.Handler {
	if base == nil {
		base = NewCommonLineHandler(&bytes.Buffer{}, slog.LevelInfo)
	}
	state := &sessionBufferController{uiMode: uiMode}
	h := &sessionBufferedHandler{base: base, state: state}
	if uiMode != nil {
		uiMode.OnModeChange(state.onModeChange)
	}
	return h
}

type sessionBufferedHandler struct {
	base  slog.Handler
	state *sessionBufferController
}

type sessionBufferController struct {
	uiMode sessionBufferedSurface
	mu     sync.Mutex
	// Shared across handler derivatives produced by WithAttrs/WithGroup so one
	// interactive period captures all records regardless of callsite scoping.
	buffer []bufferedRecord
}

type bufferedRecord struct {
	handler slog.Handler
	record  slog.Record
}

func (h *sessionBufferedHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h == nil || h.base == nil {
		return false
	}
	return h.base.Enabled(ctx, level)
}

func (h *sessionBufferedHandler) Handle(ctx context.Context, r slog.Record) error {
	mode := tuisession.ModeTranscript
	if h.state != nil && h.state.uiMode != nil {
		mode = h.state.uiMode.Mode()
	}
	if mode == tuisession.ModeInteractive {
		// Keep the fully scoped base handler so flush replays with the exact
		// attrs/groups the record had when it was originally emitted.
		h.state.mu.Lock()
		h.state.buffer = append(h.state.buffer, bufferedRecord{handler: h.base, record: cloneRecord(r)})
		h.state.mu.Unlock()
		return nil
	}
	return h.base.Handle(ctx, r)
}

func (h *sessionBufferedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &sessionBufferedHandler{
		base:  h.base.WithAttrs(attrs),
		state: h.state,
	}
}

func (h *sessionBufferedHandler) WithGroup(name string) slog.Handler {
	return &sessionBufferedHandler{
		base:  h.base.WithGroup(name),
		state: h.state,
	}
}

func (s *sessionBufferController) onModeChange(prev, next tuisession.Mode) {
	if prev != tuisession.ModeInteractive || next != tuisession.ModeTranscript {
		return
	}
	s.mu.Lock()
	pending := append([]bufferedRecord(nil), s.buffer...)
	s.buffer = nil
	s.mu.Unlock()
	for _, item := range pending {
		_ = item.handler.Handle(context.Background(), item.record)
	}
}

func cloneRecord(r slog.Record) slog.Record {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(a)
		return true
	})
	return out
}
