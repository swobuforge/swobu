package ports

import (
	"context"

	"github.com/metrofun/swobu/internal/domain/endpointintent"
)

// EndpointIntentRepository owns durable endpoint-intent persistence. It
// exposes endpoint-oriented reads plus snapshot writes so storage can stay
// atomic without leaking file mechanics into app or domain packages.
type EndpointIntentRepository interface {
	EndpointReader
	ListEndpoints(ctx context.Context) ([]endpointintent.Endpoint, error)
	SaveEndpoints(ctx context.Context, endpoints []endpointintent.Endpoint) error
}
