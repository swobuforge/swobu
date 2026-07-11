package turnstate

import "context"

// Store persists opaque continuation payloads.
type Store interface {
	Put(ctx context.Context, key Key, value []byte) error
	Get(ctx context.Context, key Key) ([]byte, bool, error)
}
