package bootstrap

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/runtimeevidence"
	"github.com/swobuforge/swobu/internal/ports"
)

// telemetryObservedRequestEvidenceSink decorates runtime evidence writes with a
// downstream telemetry observer callback. It must never block or alter the
// request-path evidence append semantics.
type telemetryObservedRequestEvidenceSink struct {
	base     ports.RequestEvidenceSink
	onAppend func(runtimeevidence.TrafficEvent)
}

func newTelemetryObservedEvidenceSink(base ports.RequestEvidenceSink, onAppend func(runtimeevidence.TrafficEvent)) ports.RequestEvidenceSink {
	return &telemetryObservedRequestEvidenceSink{
		base:     base,
		onAppend: onAppend,
	}
}

func (s *telemetryObservedRequestEvidenceSink) Append(ctx context.Context, event runtimeevidence.TrafficEvent) {
	if s == nil || s.base == nil {
		return
	}
	s.base.Append(ctx, event)
	if s.onAppend != nil {
		s.onAppend(event)
	}
}
