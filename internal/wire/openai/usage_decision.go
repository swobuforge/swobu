package openai

import (
	"context"

	"github.com/swobuforge/swobu/internal/compat"
)

// EmitUsageDecision commits an exact compatibility decision when a
// usage fact is present.
func EmitUsageDecision(ctx context.Context, sink compat.Sink, exchangeID string, present bool, feature compat.Feature, subject compat.Subject) {
	if sink == nil || !present {
		return
	}
	_ = sink.Commit(ctx, exchangeID, []compat.Decision{
		compat.Decision{
			Feature: feature,
			Outcome: compat.Exact,
			Subject: subject,
		},
	})
}
