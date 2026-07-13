package openai

import (
	"context"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/effect"
)

// EmitUsageCompatibilityEffect commits an exact compatibility effect when a
// usage fact is present.
func EmitUsageCompatibilityEffect(ctx context.Context, sink effect.Sink, exchangeID string, present bool, feature compat.Feature, subject compat.Subject) {
	if sink == nil || !present {
		return
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.CompatibilityEffect{
			Feature: feature,
			Outcome: compat.Exact,
			Subject: subject,
		},
	})
}
