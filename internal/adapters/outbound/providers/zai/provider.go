// Package zai owns the two-access Z.AI provider and its one demonstrated
// divergence from standard Chat Completions request grammar.
package zai

import (
	"context"
	"net/http"

	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/wire/chatcompletions"
)

// NewRuntime composes Z.AI Bearer transport, manual model entry, and the exact
// hosted-search request rewrite for both access products.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	bundle := openaifamily.NewRuntime(client, credentials, openaifamily.NewZAIPolicy())
	bundle.BackendResolver = backendResolver{standard: bundle.BackendResolver}
	bundle.Discovery = unsupportedDiscovery{}
	return bundle
}

type unsupportedDiscovery struct{}

func (unsupportedDiscovery) ProbeTarget(context.Context, provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	return provider.TargetProbeResult{}, canonical.NotImplemented("Swobu does not implement Z.AI model discovery")
}

type backendResolver struct{ standard provider.BackendResolver }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	backend, err := r.standard.ResolveBackend(target)
	if err != nil {
		return provider.Backend{}, err
	}
	backend.Codec = codec{}
	return backend, backend.Validate()
}

type codec struct{}

type requestTool struct {
	chatcompletions.ProviderRequestTool
	WebSearch *webSearchOptions `json:"web_search,omitempty"`
}

type webSearchOptions struct {
	Enable bool `json:"enable"`
}

func (c codec) Encode(req provider.Request) (carrier.Document, []compat.Decision, error) {
	document, decisions, err := protocolcodec.LowerChatCompletionsRequest(req)
	if err != nil {
		return carrier.Document{}, decisions, err
	}

	if err := rewriteWebSearch(&document); err != nil {
		return carrier.Document{}, decisions, err
	}

	encoded, err := chatcompletions.EncodeProviderRequestDocument(document)
	return encoded, decisions, err
}

func rewriteWebSearch(document *chatcompletions.ProviderRequestDocument) error {
	raw, hasWebSearch := document.Payload["web_search_options"]
	if !hasWebSearch {
		return nil
	}
	options, ok := raw.(map[string]any)
	if !ok || len(options) != 0 {
		return provider.NewIncompatibleTarget("Z.AI target cannot represent the requested canonical web-search options")
	}
	delete(document.Payload, "web_search_options")
	tools := make([]requestTool, 0, len(document.Tools)+1)
	for _, tool := range document.Tools {
		tools = append(tools, requestTool{ProviderRequestTool: tool})
	}
	tools = append(tools, requestTool{
		ProviderRequestTool: chatcompletions.ProviderRequestTool{Type: canonical.ToolTypeWebSearch},
		WebSearch:           &webSearchOptions{Enable: true},
	})
	document.ReplaceTools(tools)
	return nil
}

func (codec) Decode(ctx context.Context, req provider.Request, ingress provider.Ingress) (provider.DecodedResponse, error) {
	return (protocolcodec.Codec{Protocol: protocolkind.ChatCompletions}).Decode(ctx, req, ingress)
}

var _ provider.Discovery = unsupportedDiscovery{}
var _ provider.Codec = codec{}
