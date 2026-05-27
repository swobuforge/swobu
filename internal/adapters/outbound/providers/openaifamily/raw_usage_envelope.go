package openaifamily

import (
	"net/http"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
)

// RawUsageEnvelope carries raw usage-bearing payload/headers from protocol decode
// boundaries so route profiles can normalize provider-specific accounting.
type RawUsageEnvelope struct {
	ProviderID providercatalog.ProviderID
	Protocol   protocolkind.ProtocolKind
	Body       []byte
	Headers    http.Header
}
