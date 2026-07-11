package exchange

import "github.com/swobuforge/swobu/internal/transform"

type Stage = transform.Stage

const (
	StageClientHTTPIn    = transform.StageClientHTTPIn
	StageClientWireIn    = transform.StageClientWireIn
	StageSemanticRequest = transform.StageSemanticRequest

	StageProviderWireOut = transform.StageProviderWireOut
	StageProviderHTTPOut = transform.StageProviderHTTPOut

	StageProviderHTTPIn = transform.StageProviderHTTPIn
	StageProviderWireIn = transform.StageProviderWireIn
	StageSemanticEvents = transform.StageSemanticEvents

	StageClientWireOut = transform.StageClientWireOut
	StageClientHTTPOut = transform.StageClientHTTPOut
)
