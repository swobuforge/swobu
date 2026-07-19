package exchange

import (
	"context"
	"strings"
	"time"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/observation"
)

// compatibilityObservationSink is the concrete persistence boundary for
// descriptive compatibility evidence. Its type cannot carry execution state.
type compatibilityObservationSink struct{ store observation.Store }

func (s compatibilityObservationSink) Commit(ctx context.Context, _ string, decisions []compat.Decision) error {
	for _, decision := range decisions {
		reason := strings.TrimSpace(string(decision.Outcome))  // swobu:io-string source=boundary
		subject := strings.TrimSpace(string(decision.Subject)) // swobu:io-string source=boundary
		if subject != "" {
			if reason != "" {
				reason += " "
			}
			reason += subject
		}
		if err := s.store.Put(ctx, observation.ObservationRecord{
			Code:       strings.TrimSpace(string(decision.Feature)), // swobu:io-string source=boundary
			Reason:     reason,
			ObservedAt: time.Now().Unix(),
		}); err != nil {
			return err
		}
	}
	return nil
}

var _ compat.Sink = compatibilityObservationSink{}
