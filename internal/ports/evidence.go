package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/domain/runtimeevidence"
)

type RequestEvidenceSink interface {
	// Append records one immutable traffic event in the runtime evidence plane.
	Append(ctx context.Context, event runtimeevidence.TrafficEvent)
}
