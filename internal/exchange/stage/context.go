package stage

import (
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// Context carries exchange facts for patch and wrapper evaluation.
// It stays focused on lookup facts, not carrier payload metadata or services.
type Context struct {
	ExchangeID string
	Carrier    carrier.Kind
	Family     protocolkind.ProtocolKind
	Delivery   delivery.Delivery
}
