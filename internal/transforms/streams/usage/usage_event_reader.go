package usage

import (
	"context"
	"io"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// UsageEventReader emits at most one terminal usage event per response envelope while
// preserving non-usage event order and payload identity.
type UsageEventReader struct {
	inner        canonical.EventReader
	pending      canonical.EventSequence
	terminalByID map[canonical.EnvelopeID]canonical.Event
	order        []canonical.EnvelopeID
}

func Wrap(inner canonical.EventReader) canonical.EventReader {
	if inner == nil {
		return nil
	}
	return &UsageEventReader{inner: inner, terminalByID: map[canonical.EnvelopeID]canonical.Event{}}
}

func (r *UsageEventReader) Next(ctx context.Context) (canonical.Event, error) {
	if len(r.pending) > 0 {
		ev := r.pending[0]
		r.pending = r.pending[1:]
		return ev, nil
	}
	for {
		ev, err := r.inner.Next(ctx)
		if err != nil {
			if err == io.EOF {
				for _, id := range r.order {
					if pending, ok := r.terminalByID[id]; ok {
						r.pending = append(r.pending, pending)
					}
				}
				r.terminalByID = map[canonical.EnvelopeID]canonical.Event{}
				r.order = r.order[:0]
				if len(r.pending) > 0 {
					out := r.pending[0]
					r.pending = r.pending[1:]
					return out, nil
				}
			}
			return canonical.Event{}, err
		}
		if ev.Kind == canonical.EventUsage {
			if _, ok := r.terminalByID[ev.EnvID]; !ok {
				r.order = append(r.order, ev.EnvID)
			}
			r.terminalByID[ev.EnvID] = ev
			continue
		}
		if ev.Kind == canonical.EventEnvelopeEnd {
			if payload, ok := ev.Payload.(canonical.EnvelopeEndPayload); ok && payload.Kind == canonical.EnvResponse {
				if u, ok := r.terminalByID[ev.EnvID]; ok {
					r.pending = append(r.pending, u)
					delete(r.terminalByID, ev.EnvID)
				}
				r.pending = append(r.pending, ev)
				out := r.pending[0]
				r.pending = r.pending[1:]
				return out, nil
			}
		}
		return ev, nil
	}
}

func (r *UsageEventReader) Close(ctx context.Context) error {
	return r.inner.Close(ctx)
}
