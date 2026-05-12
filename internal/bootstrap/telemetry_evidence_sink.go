package bootstrap

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/runtimeevidence"
	"github.com/swobuforge/swobu/internal/ports"
)

// telemetryObservedEvidenceSink decorates runtime evidence writes with a
// downstream telemetry observer callback. It must never block or alter the
// request-path evidence append semantics.
type telemetryObservedEvidenceSink struct {
	base     ports.RequestEvidenceSink
	onAppend func(runtimeevidence.TrafficEvent)
}

func newTelemetryObservedEvidenceSink(base ports.RequestEvidenceSink, onAppend func(runtimeevidence.TrafficEvent)) ports.RequestEvidenceSink {
	return &telemetryObservedEvidenceSink{
		base:     base,
		onAppend: onAppend,
	}
}

func (s *telemetryObservedEvidenceSink) Append(ctx context.Context, event runtimeevidence.TrafficEvent) {
	if s == nil || s.base == nil {
		return
	}
	s.base.Append(ctx, event)
	if s.onAppend != nil {
		s.onAppend(event)
	}
}
