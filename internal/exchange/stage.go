package exchange

import "github.com/swobuforge/swobu/internal/transform"

type Stage = transform.Stage

const (
	StageClientWireIn    = transform.StageClientWireIn
	StageProviderWireOut = transform.StageProviderWireOut
	StageProviderWireIn  = transform.StageProviderWireIn
	StageSemanticEvents  = transform.StageSemanticEvents
)
