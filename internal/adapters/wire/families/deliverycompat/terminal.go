package deliverycompat

import (
	"context"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/effect"
)

const terminalEventSubject = compat.Subject("wire:/event/terminal")

// EmitTerminalEventDecision records whether the stream reached a terminal
// completion with terminal usage present.
func EmitTerminalEventDecision(ctx context.Context, sink effect.Sink, exchangeID string, exact bool) {
	if sink == nil {
		return
	}
	outcome := compat.Drop
	if exact {
		outcome = compat.Exact
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.Compatibility{
			Feature: compat.DeliveryTerminalEvent,
			Outcome: outcome,
			Subject: terminalEventSubject,
		},
	})
}
