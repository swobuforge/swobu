package compat

import "context"

// Sink persists or locally accumulates compatibility decisions only. It is not
// an execution-effect bus and cannot carry replay, provider state, or control.
type Sink interface {
	Commit(context.Context, string, []Decision) error
}

type NoopSink struct{}

func (NoopSink) Commit(context.Context, string, []Decision) error { return nil }

// AccumulatorSink collects decisions within one concrete codec operation.
type AccumulatorSink struct{ Decisions *[]Decision }

func (s AccumulatorSink) Commit(_ context.Context, _ string, decisions []Decision) error {
	if s.Decisions != nil {
		*s.Decisions = append(*s.Decisions, decisions...)
	}
	return nil
}

// RecordingSink exposes decisions discovered during progressive consumption.
type RecordingSink struct {
	Delegate  Sink
	decisions []Decision
}

func (s *RecordingSink) Commit(ctx context.Context, exchangeID string, decisions []Decision) error {
	s.decisions = append(s.decisions, decisions...)
	if s.Delegate != nil {
		return s.Delegate.Commit(ctx, exchangeID, decisions)
	}
	return nil
}

func (s *RecordingSink) Decisions() []Decision {
	return append([]Decision(nil), s.decisions...)
}
