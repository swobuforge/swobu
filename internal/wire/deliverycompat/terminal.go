package deliverycompat

import (
	"context"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/effect"
)

const terminalEventSubject = compat.Subject("wire:/event/terminal")

// EmitTerminalUsagePresence records whether the terminal completion carried
// usage. It is an observability signal, not a lifecycle transition.
func EmitTerminalUsagePresence(ctx context.Context, sink effect.Sink, exchangeID string, exact bool) {
	if sink == nil {
		return
	}
	outcome := compat.Drop
	if exact {
		outcome = compat.Exact
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.CompatibilityEffect{
			Feature: compat.DeliveryTerminalEvent,
			Outcome: outcome,
			Subject: terminalEventSubject,
		},
	})
}
