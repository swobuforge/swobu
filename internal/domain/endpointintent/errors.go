package endpointintent

import "errors"

var (
	ErrInvalidEndpointName      = errors.New("invalid endpoint name")
	ErrInvalidProviderSpec      = errors.New("invalid provider spec")
	ErrInvalidProviderConfigRef = errors.New("invalid provider config ref")
	ErrInvalidProviderConfig    = errors.New("invalid provider config")
	ErrInvalidEndpoint          = errors.New("invalid endpoint")
)
