package custom

import (
	"context"
	"fmt"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
)

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	standard, ok := backend.Codec.(protocolcodec.Codec)
	if !ok {
		return provider.Backend{}, fmt.Errorf("custom backend has codec %T, want protocolcodec.Codec", backend.Codec)
	}
	backend.Codec = webSearchBackendCodec{standard: standard, protocol: target.ProtocolKind}
	return backend, backend.Validate()
}

type webSearchBackendCodec struct {
	standard protocolcodec.Codec
	protocol protocolkind.ProtocolKind
}

func (c webSearchBackendCodec) Encode(req provider.Request) (carrier.Document, []compat.Decision, error) {
	if c.protocol != protocolkind.Responses {
		for _, tool := range req.Canonical.Tools() {
			if tool.Kind() == canonical.ToolKindWebSearch {
				return carrier.Document{}, nil, provider.UnsupportedByBackend(canonical.UnsupportedOperation("custom target protocol does not support provider-hosted web search"))
			}
		}
	}
	return c.standard.Encode(req)
}

func (c webSearchBackendCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.standard.Decode(ctx, req, ingress)
}

var _ provider.BackendResolver = backendResolver{}
var _ provider.Codec = webSearchBackendCodec{}
