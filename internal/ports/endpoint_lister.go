package ports

import (
	"context"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
)

// EndpointLister returns the durable endpoint-intent snapshot used by
// operator-support read paths.
type EndpointLister interface {
	ListEndpoints(ctx context.Context) ([]endpointintent.Endpoint, error)
}
