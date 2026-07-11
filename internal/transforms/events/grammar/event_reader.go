package grammar

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// Wrap validates envelope start/end grammar lazily while preserving event order.
func Wrap(inner canonical.EventReader) canonical.EventReader {
	if inner == nil {
		return nil
	}
	return &EventReader{inner: inner, stateByID: map[canonical.EnvelopeID]canonical.EnvelopeKind{}}
}

type EventReader struct {
	inner     canonical.EventReader
	stateByID map[canonical.EnvelopeID]canonical.EnvelopeKind
}

func (r *EventReader) Next(ctx context.Context) (canonical.Event, error) {
	ev, err := r.inner.Next(ctx)
	if err != nil {
		if err == io.EOF && len(r.stateByID) > 0 {
			return canonical.Event{}, fmt.Errorf("envelope stream ended with open envelopes: %s", openEnvelopeIDs(r.stateByID))
		}
		return canonical.Event{}, err
	}
	switch ev.Kind {
	case canonical.EventEnvelopeStart:
		payload, ok := ev.Payload.(canonical.EnvelopeStartPayload)
		if !ok {
			return canonical.Event{}, fmt.Errorf("envelope start missing payload")
		}
		if _, exists := r.stateByID[ev.EnvID]; exists {
			return canonical.Event{}, fmt.Errorf("envelope start duplicated for %q", ev.EnvID)
		}
		r.stateByID[ev.EnvID] = payload.Kind
	case canonical.EventEnvelopeEnd:
		payload, ok := ev.Payload.(canonical.EnvelopeEndPayload)
		if !ok {
			return canonical.Event{}, fmt.Errorf("envelope end missing payload")
		}
		startedKind, exists := r.stateByID[ev.EnvID]
		if !exists {
			return canonical.Event{}, fmt.Errorf("envelope end without start for %q", ev.EnvID)
		}
		if startedKind != payload.Kind {
			return canonical.Event{}, fmt.Errorf("envelope kind mismatch for %q", ev.EnvID)
		}
		delete(r.stateByID, ev.EnvID)
	}
	return ev, nil
}

func (r *EventReader) Close(ctx context.Context) error {
	if len(r.stateByID) > 0 {
		return fmt.Errorf("cannot close with open envelopes: %s", openEnvelopeIDs(r.stateByID))
	}
	err := r.inner.Close(ctx)
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}

func openEnvelopeIDs(state map[canonical.EnvelopeID]canonical.EnvelopeKind) string {
	if len(state) == 0 {
		return ""
	}
	ids := make([]string, 0, len(state))
	for id := range state {
		ids = append(ids, strings.TrimSpace(string(id))) // swobu:io-string source=boundary
	}
	return strings.Join(ids, ",")
}
