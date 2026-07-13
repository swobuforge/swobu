package deliverycompat

import (
	"context"
	"testing"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/effect"
)

type recordingSink struct {
	effects []effect.Effect
}

func (s *recordingSink) Commit(_ context.Context, _ string, effects []effect.Effect) error {
	s.effects = append([]effect.Effect(nil), effects...)
	return nil
}

func TestEmitTerminalUsagePresence(t *testing.T) {
	exactSink := &recordingSink{}
	EmitTerminalUsagePresence(context.Background(), exactSink, "ex_terminal_exact", true)
	if len(exactSink.effects) != 1 {
		t.Fatalf("exact captured effects len=%d want=1", len(exactSink.effects))
	}
	exactEffect, ok := exactSink.effects[0].(effect.CompatibilityEffect)
	if !ok {
		t.Fatalf("exact effect type = %T, want effect.CompatibilityEffect", exactSink.effects[0])
	}
	if exactEffect.Feature != compat.DeliveryTerminalEvent || exactEffect.Outcome != compat.Exact || exactEffect.Subject != compat.Subject("wire:/event/terminal") {
		t.Fatalf("exact compatibility effect = %#v, want delivery.terminal_event exact wire:/event/terminal", exactEffect)
	}

	dropSink := &recordingSink{}
	EmitTerminalUsagePresence(context.Background(), dropSink, "ex_terminal_drop", false)
	if len(dropSink.effects) != 1 {
		t.Fatalf("drop captured effects len=%d want=1", len(dropSink.effects))
	}
	dropEffect, ok := dropSink.effects[0].(effect.CompatibilityEffect)
	if !ok {
		t.Fatalf("drop effect type = %T, want effect.CompatibilityEffect", dropSink.effects[0])
	}
	if dropEffect.Feature != compat.DeliveryTerminalEvent || dropEffect.Outcome != compat.Drop || dropEffect.Subject != compat.Subject("wire:/event/terminal") {
		t.Fatalf("drop compatibility effect = %#v, want delivery.terminal_event drop wire:/event/terminal", dropEffect)
	}
}
