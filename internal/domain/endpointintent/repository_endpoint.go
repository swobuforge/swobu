package endpointintent

import "context"

// EndpointIntentRepository owns durable endpoint-intent persistence. It
// exposes endpoint-oriented reads plus snapshot writes so storage can stay
// atomic without leaking file mechanics into app or domain packages.
type EndpointIntentRepository interface {
	EndpointReader
	EndpointLister
	SaveEndpoints(ctx context.Context, endpoints []Endpoint) error
}
