package gmi

import (
	"net/http"

	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

// NewRuntime keeps GMI's shared protocol grammars intact and decorates only
// the documented Responses web-search type spelling.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.GMIPolicy())
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	return bundle
}

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil || target.ProtocolKind != protocolkind.Responses {
		return backend, err
	}
	backend.Codec = protocolcodec.Codec{
		Protocol: protocolkind.Responses,
		ResponsesDialect: protocolcodec.ResponsesDialect{
			Tools: protocolcodec.ResponsesToolLowering{WebSearch: protocolcodec.ResponsesHostedSearchTool("web_search_preview")},
		},
	}
	return backend, backend.Validate()
}
