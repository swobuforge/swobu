package ports

import (
	"context"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
)

type EndpointReader interface {
	// GetEndpoint returns durable endpoint intent by canonical endpoint name.
	// Implementations must not auto-create or infer missing endpoints.
	GetEndpoint(ctx context.Context, name endpointintent.EndpointName) (endpointintent.Endpoint, error)
}
