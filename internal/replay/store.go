package replay

import (
	"context"
	"errors"
)

// ErrReplayRecordExists reports that Put was asked to overwrite an existing
// replay record under the same scoped ID.
var ErrReplayRecordExists = errors.New("replay record already exists")

// Store persists replay records in workspace-slug partitions. The caller must
// pass the validated slug resolved from the request URL; replay owns no caller,
// session, or generic namespace identity.
type Store interface {
	// Get returns one record by workspace slug and ID.
	// The bool indicates whether the record was found.
	Get(ctx context.Context, workspaceSlug string, id ID) (Record, bool, error)
	// Put writes one record under the given workspace slug.
	// Duplicate IDs are rejected.
	Put(ctx context.Context, workspaceSlug string, record Record) error
}
