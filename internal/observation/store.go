package observation

import "context"

// Store persists and queries runtime observations.
type Store interface {
	Put(ctx context.Context, obs ObservationRecord) error
	Query(ctx context.Context, q ObservationQuerySpec) ([]ObservationRecord, error)
}
