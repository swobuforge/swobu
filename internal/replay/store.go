package replay

import (
	"context"
	"errors"
)

// ErrReplayRecordExists reports that Put was asked to overwrite an existing
// replay record under the same scoped ID.
var ErrReplayRecordExists = errors.New("replay record already exists")

// Store persists and retrieves replay records scoped to one namespace/caller.
type Store interface {
	// Get returns one record by scope and ID.
	// The bool indicates whether the record was found.
	Get(ctx context.Context, scope Scope, id ID) (Record, bool, error)
	// Put writes one record under the given scope.
	// Duplicate IDs are rejected.
	Put(ctx context.Context, scope Scope, record Record) error
}
