package exchange

import "github.com/swobuforge/swobu/internal/transform"

type Stage = transform.Stage

const (
	StageClientWireIn       = transform.StageClientWireIn
	StageRequestDocumentOut = transform.StageRequestDocumentOut
	StageRequestDocumentIn  = transform.StageRequestDocumentIn
	StageSemanticEvents     = transform.StageSemanticEvents
)
