package endpointintent

import (
	"context"
)

type EndpointReader interface {
	// GetEndpoint returns durable endpoint intent by canonical endpoint name.
	// Implementations must not auto-create or infer missing endpoints.
	GetEndpoint(ctx context.Context, name EndpointName) (Endpoint, error)
}
