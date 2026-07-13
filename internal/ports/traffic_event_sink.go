package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/trafficevidence"
)

type TrafficEventSink interface {
	// Append records one immutable traffic event in the traffic evidence plane.
	Append(ctx context.Context, event trafficevidence.TrafficEvent)
}
