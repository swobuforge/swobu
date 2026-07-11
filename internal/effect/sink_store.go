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
		case ObservationEffect:
			if s.Observations != nil {
				obs := stampObservationRecord(e.Observation)
				if err := s.Observations.Put(ctx, obs); err != nil {
					return err
				}
			}
		case LossEffect:
			if s.Observations != nil {
				obs := stampObservationRecord(e.Observation)
				reasonCode := strings.TrimSpace(string(e.Loss.ReasonCode)) // swobu:io-string source=boundary
				if obs.Code == "" {
					obs.Code = reasonCode
				}
				if obs.Reason == "" {
					obs.Reason = strings.TrimSpace(e.Loss.Reason) // swobu:io-string source=boundary
				}
				if obs.Code == "" {
					obs.Code = "loss"
				}
				if err := s.Observations.Put(ctx, obs); err != nil {
					return err
				}
			}
		case StateCaptureEffect:
			if s.TurnState != nil {
				if err := s.TurnState.Put(ctx, e.Key, append([]byte(nil), e.Value...)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func stampObservationRecord(obs observation.ObservationRecord) observation.ObservationRecord {
	if obs.ObservedAt == 0 {
		obs.ObservedAt = time.Now().Unix()
	}
	return obs
}
