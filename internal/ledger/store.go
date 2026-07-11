package ledger

import "context"

// Store is the single append/list contract for runtime records.
type Store[T any] interface {
	Append(ctx context.Context, record T)
	List(ctx context.Context, limit int) ([]T, error)
}
