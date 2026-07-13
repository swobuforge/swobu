package endpointintent

import "context"

// EndpointLister returns the durable endpoint-intent snapshot used by
// operator-support read paths.
type EndpointLister interface {
	ListEndpoints(ctx context.Context) ([]Endpoint, error)
}
