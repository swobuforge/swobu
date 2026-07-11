package transform

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// Context carries stage-scoped exchange facts for transform evaluation.
type Context struct {
	ExchangeID   string
	Stage        Stage
	CarrierStage carrier.Stage
	Carrier      carrier.Kind
	Family       protocolkind.ProtocolKind
	Delivery     delivery.Delivery
}
