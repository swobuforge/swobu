package openaifamily

import (
	"net/http"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
)

// RawUsageEnvelope carries raw usage-bearing payload/headers from protocol decode
// boundaries so route profiles can normalize provider-specific accounting.
type RawUsageEnvelope struct {
	ProviderID profile.ProviderID
	Protocol   protocolkind.ProtocolKind
	Body       []byte
	Headers    http.Header
}
