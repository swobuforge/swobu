package ports

import (
	"context"

	"github.com/swobuforge/swobu/internal/evidence"
)

type RequestEvidenceSink interface {
	// Append records one immutable traffic event in the runtime evidence plane.
	Append(ctx context.Context, event evidence.TrafficEvent)
}
