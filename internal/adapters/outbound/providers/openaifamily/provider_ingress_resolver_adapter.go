// selection, wire realization, and response decoding in one outbound seam.
package openaifamily

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	chatcompletions "github.com/swobuforge/swobu/internal/adapters/wire/families/chatcompletions"
	completions "github.com/swobuforge/swobu/internal/adapters/wire/families/completions"
	messages "github.com/swobuforge/swobu/internal/adapters/wire/families/messages"
	responses "github.com/swobuforge/swobu/internal/adapters/wire/families/responses"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/protocolsurface"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

type ProviderIngressResolverAdapter struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
	profile     ProviderRoutePolicy
}

const swobuCallerUAHeaderValue = "swobu/dev"

// NewExecutor builds the OpenAI-family provider wiring adapter around commodity HTTP transport.
func NewExecutor(client *http.Client, credentials providersruntime.CredentialProvider, profile ProviderRoutePolicy) ProviderIngressResolverAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	if profile == nil {
		panic("openaifamily: route profile is required")
	}
	return ProviderIngressResolverAdapter{
		client:      client,
		credentials: credentials,
		profile:     profile,
	}
}

// NewRuntime builds a complete OpenAI-family provider runtime for one provider policy.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider, profile ProviderRoutePolicy) providersruntime.ProviderRuntimeBundle {
	executor := NewExecutor(client, credentials, profile)
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         profile.ProviderID(),
		IngressResolver:    executor,
		CredentialProvider: credentials,
		ModelCatalogClient: executor,
	}
}

// Execute performs provider HTTP transport only. Exchange orchestration,
// transforms, and semantic decode live in exchange.
func (e ProviderIngressResolverAdapter) ResolveProviderIngress(ctx context.Context, req ports.ProviderRequest) (ports.ProviderIngress, error) {
	if strings.TrimSpace(req.Request.Model()) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadRequest("canonical request is required")
	}
	if strings.TrimSpace(req.Target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("OpenAI-family provider base URL is required")
	}
	if requiresExplicitCredentialRef(req.Target.ProviderID(), req.Target.BaseURL, req.Target.CredentialRef) {
		return nil, canonical.BadEndpoint(providerCredentialRequiredMessage(req.Target.ProviderID()))
	}

	if parsed, ok := profile.ParseProviderID(strings.TrimSpace(req.Target.ProviderID())); !ok || parsed != e.profile.ProviderID() { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("provider policy is unsupported for OpenAI-family adapter runtime")
	}
	if err := req.Contract.ProviderDelivery.Validate(); err != nil {
		return nil, canonical.UnsupportedDelivery("OpenAI-family provider delivery is unsupported")
	}
	if req.Contract.ProviderDelivery.IsStreaming() && req.Contract.ProviderDelivery.Framing != protocolsurface.FramingSSE {
		return nil, canonical.UnsupportedDelivery("OpenAI-family provider does not implement the requested delivery framing")
	}
	wireReqCarrier := req.RequestDocument
	if wireReqCarrier.IsEmpty() {
		codec, codecErr := providerRequestEncoderForProtocol(req.Target.ProtocolKind)
		if codecErr != nil {
			if req.Target.ProtocolKind == protocolkind.Messages {
				return nil, canonical.UnsupportedOperation("OpenAI-family provider does not implement the messages protocol")
			}
			return nil, codecErr
		}
		encoded, encodeErr := codec.EncodeProviderRequestDocument(req.Request, toInternalDelivery(req.Contract.ProviderDelivery))
		if encodeErr != nil {
			return nil, encodeErr
		}
		wireReqCarrier = encoded
	}
	path, err := providerRequestPathForProtocol(req.Target.ProtocolKind)
	if err != nil {
		return nil, err
	}
	wireReqBody := wireReqCarrier.RawBytes()
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		httpedge.JoinBaseURLAndPath(req.Target.BaseURL, path),
		bytes.NewReader(wireReqBody),
	)
	if err != nil {
		return nil, canonical.BadEndpoint("OpenAI-family provider request could not be built")
	}
	if len(wireReqBody) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)

	if err := e.applyCredential(ctx, httpReq, req.Target.ProviderID(), req.Target.CredentialRef); err != nil {
		return nil, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("OpenAI-family provider request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
		return nil, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	if resp.StatusCode >= 400 {
		defer func() {
			_ = resp.Body.Close()
		}()
		backendErr := httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
		return nil, classifyBackendError(backendErr)
	}
	if req.Contract.ProviderDelivery.IsStreaming() {
		return carrier.WireStream{
			Stage:   carrier.StageProviderIngressIn,
			Family:  req.Target.ProtocolKind,
			Framing: carrier.FramingSSE,
			Header:  resp.Header.Clone(),
			Frames:  carrier.FrameReaderFromReadCloser(resp.Body),
		}, nil
	}
	raw, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, canonical.InternalError("backend success response could not be read")
	}
	return carrier.NewWireDocument(
		carrier.StageProviderIngressIn,
		req.Target.ProtocolKind,
		"application/json",
		resp.Header.Clone(),
		raw,
		carrier.Meta{},
	), nil
}

func providerRequestPathForProtocol(kind protocolkind.ProtocolKind) (string, error) {
	switch kind {
	case protocolkind.ChatCompletions:
		return "/chat/completions", nil
	case protocolkind.Responses:
		return "/responses", nil
	case protocolkind.Completions:
		return "/completions", nil
	case protocolkind.Messages:
		return "/messages", nil
	default:
		return "", canonical.UnsupportedOperation("protocol kind is not implemented")
	}
}

// applyCredential keeps auth resolution at the provider edge so canonicals and
// app orchestration never need to know provider token mechanics.
func (e ProviderIngressResolverAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string) error {
	auth := e.profile.AuthStrategy()
	if auth.Style == authStyleNone {
		return nil
	}
	if strings.TrimSpace(credentialRef) == "" { // swobu:io-string source=boundary
		return nil
	}
	if e.credentials == nil {
		return canonical.BadEndpoint("credential resolver is not configured")
	}
	token, err := e.credentials.ResolveCredential(ctx, providerSpec, credentialRef)
	if err != nil {
		return canonical.BadEndpoint("credential reference could not be resolved")
	}
	if strings.TrimSpace(token) == "" { // swobu:io-string source=boundary
		return canonical.BadEndpoint("credential reference resolved to an empty token")
	}
	auth.apply(req, token)
	return nil
}

func requiresExplicitCredentialRef(providerSpec string, baseURL string, credentialRef string) bool {
	if strings.TrimSpace(credentialRef) != "" { // swobu:io-string source=boundary
		return false
	}
	return profile.RequiresCredential(strings.TrimSpace(providerSpec), strings.TrimSpace(baseURL)) // swobu:io-string source=boundary
}

func providerCredentialRequiredMessage(providerSpec string) string {
	providerID, ok := profile.ParseProviderID(strings.TrimSpace(providerSpec)) // swobu:io-string source=boundary
	if !ok {
		return "provider credential reference is required"
	}
	return string(providerID) + " provider credential reference is required"
}

func toInternalDelivery(surface protocolsurface.Delivery) delivery.Delivery {
	switch surface.Variant {
	case protocolsurface.DeliveryVariantStreaming:
		return delivery.Delivery{Mode: delivery.Streaming, Framing: delivery.Framing(surface.Framing)}
	case protocolsurface.DeliveryVariantBuffered:
		return delivery.Delivery{Mode: delivery.Buffered, Framing: delivery.Framing(surface.Framing)}
	default:
		return delivery.Delivery{Mode: delivery.Mode(255), Framing: delivery.Framing(surface.Framing)}
	}
}

func providerRequestEncoderForProtocol(kind protocolkind.ProtocolKind) (interface {
	EncodeProviderRequestDocument(canonical.CanonicalRequest, delivery.Delivery) (carrier.WireDocument, error)
}, error) {
	switch kind {
	case protocolkind.ChatCompletions:
		return chatcompletions.ProviderRequestDocumentEncoder{}, nil
	case protocolkind.Responses:
		return responses.ProviderRequestDocumentEncoder{}, nil
	case protocolkind.Completions:
		return completions.ProviderRequestDocumentEncoder{}, nil
	case protocolkind.Messages:
		return messages.ProviderRequestDocumentEncoder{}, nil
	default:
		return nil, canonical.UnsupportedOperation("protocol kind is not implemented")
	}
}
