package gmi

import (
	"context"
	"fmt"
	"net/http"

	openaifamily "github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire/responses"
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
	standard, ok := backend.Codec.(protocolcodec.Codec)
	if !ok {
		return provider.Backend{}, fmt.Errorf("GMI Responses backend has codec %T, want protocolcodec.Codec", backend.Codec)
	}
	backend.Codec = responsesCodec{standard: standard}
	return backend, backend.Validate()
}

type responsesCodec struct{ standard protocolcodec.Codec }

func (c responsesCodec) Encode(req provider.Request) (carrier.Document, []compat.Change, error) {
	if err := protocolcodec.ValidateEncodeRequest(req); err != nil {
		return carrier.Document{}, nil, err
	}
	var changes []compat.Change
	document, err := responses.LowerProviderRequestDocument(responses.EncodeInput{Request: req.Canonical, ToolNames: req.ToolNames}, req.Delivery, &changes, req.ExchangeID, responses.EncodeOptions{})
	if err != nil {
		return carrier.Document{}, changes, err
	}
	for index := range document.Tools {
		if document.Tools[index].Type == canonical.ToolTypeWebSearch {
			document.Tools[index].Type = "web_search_preview"
		}
	}
	if choice, ok := document.ToolChoice.(map[string]any); ok && choice["type"] == canonical.ToolTypeWebSearch {
		choice["type"] = "web_search_preview"
	}
	encoded, err := responses.EncodeProviderRequestDocument(document)
	return encoded, changes, err
}

func (c responsesCodec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return c.standard.Decode(ctx, req, ingress)
}

var _ provider.Codec = responsesCodec{}
