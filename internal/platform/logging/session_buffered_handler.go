package logging

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
)

// NewBufferedHandler returns a slog handler that buffers records until Flush is called.
//
// The handler preserves attrs and groups across flush so replayed records keep the
// same call-site scoping they had when emitted.
func NewBufferedHandler(base slog.Handler) *BufferedHandler {
	if base == nil {
		base = slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	return &BufferedHandler{
		base:  base,
		state: &bufferState{},
	}
}

// BufferedHandler buffers slog records and replays them later through the
// original base handler.
type BufferedHandler struct {
	base  slog.Handler
	state *bufferState
}

type bufferState struct {
	mu     sync.Mutex
	buffer []bufferedRecord
}

type bufferedRecord struct {
	handler slog.Handler
	record  slog.Record
}

func (h *BufferedHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h == nil || h.base == nil {
		return false
	}
	return h.base.Enabled(ctx, level)
}

func (h *BufferedHandler) Handle(ctx context.Context, r slog.Record) error {
	if h == nil || h.state == nil || h.base == nil {
		return nil
	}
	// Keep the fully scoped base handler so flush replays with the exact attrs/groups
	// the record had when it was originally emitted.
	h.state.mu.Lock()
	h.state.buffer = append(h.state.buffer, bufferedRecord{handler: h.base, record: cloneRecord(r)})
	h.state.mu.Unlock()
	return nil
}

func (h *BufferedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &BufferedHandler{
		base:  h.base.WithAttrs(attrs),
		state: h.state,
	}
}

func (h *BufferedHandler) WithGroup(name string) slog.Handler {
	return &BufferedHandler{
		base:  h.base.WithGroup(name),
		state: h.state,
	}
}

// Flush replays buffered records through the original handler chain and clears the buffer.
func (h *BufferedHandler) Flush(ctx context.Context) {
	if h == nil || h.state == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	h.state.mu.Lock()
	pending := append([]bufferedRecord(nil), h.state.buffer...)
	h.state.buffer = nil
	h.state.mu.Unlock()
	for _, item := range pending {
		_ = item.handler.Handle(ctx, item.record)
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
