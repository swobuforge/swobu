package effect

import (
	"context"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/turnstate"
)

type NoopSink struct{}

func (NoopSink) Commit(context.Context, string, []Effect) error { return nil }

type StoreBackedSink struct {
	Observations observation.Store
	TurnState    turnstate.TurnStateStore
}

func (s StoreBackedSink) Commit(ctx context.Context, _ string, effects []Effect) error {
	for _, applied := range effects {
		switch e := applied.(type) {
		case CompatibilityEffect:
			if s.Observations != nil {
				obs := compatibilityObservationRecord(e)
				if err := s.Observations.Put(ctx, stampObservationRecord(obs)); err != nil {
					return err
				}
			}
		case TurnStateEffect:
			if s.TurnState != nil {
				key := turnstate.TurnStateKey{
					Subject: e.Key,
					Kind:    turnstate.TurnStateKind(e.Op),
				}
				if err := s.TurnState.Put(ctx, key, append([]byte(nil), e.Value...)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func compatibilityObservationRecord(decision CompatibilityEffect) observation.ObservationRecord {
	code := strings.TrimSpace(string(decision.Feature))    // swobu:io-string source=boundary
	reason := strings.TrimSpace(string(decision.Outcome))  // swobu:io-string source=boundary
	subject := strings.TrimSpace(string(decision.Subject)) // swobu:io-string source=boundary
	if subject != "" {
		if reason != "" {
			reason += " "
		}
		reason += subject
	}
	return observation.ObservationRecord{
		Code:   code,
		Reason: reason,
	}
}

func stampObservationRecord(obs observation.ObservationRecord) observation.ObservationRecord {
	if obs.ObservedAt == 0 {
		obs.ObservedAt = time.Now().Unix()
	}
	return obs
}
