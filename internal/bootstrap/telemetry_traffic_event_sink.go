package bootstrap

import (
	"context"

	trafficevidence "github.com/swobuforge/swobu/internal/domain/trafficevidence"
	"github.com/swobuforge/swobu/internal/observation"
)

// telemetryObservedTrafficEventSink decorates traffic evidence writes with a
// downstream telemetry observer callback. It must never block or alter the
// request-path traffic append semantics.
type telemetryObservedTrafficEventSink struct {
	base     observation.TrafficEventSink
	onAppend func(trafficevidence.TrafficEvent)
}

func newTelemetryObservedTrafficEventSink(base observation.TrafficEventSink, onAppend func(trafficevidence.TrafficEvent)) observation.TrafficEventSink {
	return &telemetryObservedTrafficEventSink{
		base:     base,
		onAppend: onAppend,
	}
}

func (s *telemetryObservedTrafficEventSink) Append(ctx context.Context, event trafficevidence.TrafficEvent) {
	if s == nil || s.base == nil {
		return
	}
	s.base.Append(ctx, event)
	if s.onAppend != nil {
		s.onAppend(event)
	}
}
