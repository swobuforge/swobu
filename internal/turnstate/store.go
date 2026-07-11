package turnstate

import "context"

// TurnStateStore persists opaque provider-native replay bytes.
type TurnStateStore interface {
	Put(ctx context.Context, key TurnStateKey, value []byte) error
	Get(ctx context.Context, key TurnStateKey) ([]byte, bool, error)
}
