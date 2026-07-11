package anthropic

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	messages "github.com/swobuforge/swobu/internal/adapters/wire/families/messages"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

const (
	anthropicVersionHeaderValue = "2023-06-01"
	swobuCallerUAHeaderValue    = "swobu/dev"
)

type ProviderIngressResolverAdapter struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
}

func NewExecutor(client *http.Client, credentials providersruntime.CredentialProvider) ProviderIngressResolverAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return ProviderIngressResolverAdapter{
		client:      client,
		credentials: credentials,
	}
}

// NewRuntime builds a complete Anthropic provider runtime.
func NewRuntime(providerID profile.ProviderID, client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	executor := NewExecutor(client, credentials)
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         providerID,
		IngressResolver:    executor,
		CredentialProvider: credentials,
		ModelCatalogClient: executor,
	}
}

func (e ProviderIngressResolverAdapter) ResolveProviderIngress(ctx context.Context, req ports.ProviderRequest) (ports.ProviderIngress, error) {
	deliveryMode, err := resolveAnthropicProviderProtocol(req.Target.ProviderProtocol)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Request.Model()) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadRequest("canonical request is required")
	}
	if strings.TrimSpace(req.Target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("anthropic provider base URL is required")
	}

	resolvedDelivery := delivery.BufferedDelivery()
	if deliveryMode == delivery.Streaming {
		resolvedDelivery = delivery.StreamingDelivery(delivery.FramingSSE)
	}
	wireReq, err := messages.ProviderRequestDocumentEncoder{}.EncodeProviderRequestDocument(req.Request, resolvedDelivery)
	if err != nil {
		return nil, err
	}
	wireReqBody := wireReq.RawBytes()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, httpedge.JoinBaseURLAndPath(req.Target.BaseURL, "/messages"), bytes.NewReader(wireReqBody))
	if err != nil {
		return nil, canonical.BadEndpoint("anthropic provider request could not be built")
	}
	if len(wireReqBody) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("anthropic-version", anthropicVersionHeaderValue)
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, req.Target.ProviderID(), req.Target.CredentialRef); err != nil {
		return nil, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("anthropic provider request failed before backend response")
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
		return nil, httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
	}
	if deliveryMode == delivery.Streaming {
		framing := carrier.FramingSSE
		if resolvedDelivery.Framing != delivery.FramingSSE {
			framing = carrier.FramingNone
		}
		return carrier.WireStream{
			Stage:   carrier.StageProviderIngressIn,
			Family:  protocolkind.Messages,
			Framing: framing,
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
		protocolkind.Messages,
		"application/json",
		resp.Header.Clone(),
		raw,
		carrier.Meta{},
	), nil
}

func (e ProviderIngressResolverAdapter) ListModels(ctx context.Context, target exchange.RoutableTarget) ([]string, error) {
	if strings.TrimSpace(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("anthropic provider base URL is required")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, httpedge.JoinBaseURLAndPath(target.BaseURL, "/models"), nil)
	if err != nil {
		return nil, canonical.BadEndpoint("anthropic provider model catalog request could not be built")
	}
	httpReq.Header.Set("anthropic-version", anthropicVersionHeaderValue)
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, target.ProviderID(), target.CredentialRef); err != nil {
		return nil, err
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("anthropic provider model catalog request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return nil, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, httpedge.ReadBackendHTTPError(resp, target.BackendRef)
	}
	models, err := modelcatalogopenai.DecodeModelIDs(resp.Body)
	if err != nil {
		return nil, canonical.InternalError("backend model catalog could not be decoded")
	}
	return models, nil
}

func (e ProviderIngressResolverAdapter) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	_, err := e.ListModels(ctx, target)
	return err
}

func resolveAnthropicProviderProtocol(providerProtocol string) (delivery.Mode, error) {
	providerProtocol = strings.TrimSpace(providerProtocol) // swobu:io-string source=boundary
	if providerProtocol == "" || providerProtocol == profile.ProviderProtocolAuto {
		return delivery.Buffered, canonical.BadEndpoint("anthropic provider protocol must be concrete")
	}
	if !profile.SupportsProviderProtocolForSpec(string(profile.ProviderSpecAnthropic), providerProtocol) {
		return delivery.Buffered, canonical.BadEndpoint("selected provider protocol is unsupported for anthropic")
	}
	switch providerProtocol {
	case "messages":
		return delivery.Buffered, nil
	case "messages_stream":
		return delivery.Streaming, nil
	default:
		return delivery.Buffered, canonical.BadEndpoint("selected provider protocol is unsupported for anthropic")
	}
}

func (e ProviderIngressResolverAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string) error {
	if strings.TrimSpace(credentialRef) == "" { // swobu:io-string source=boundary
		return canonical.BadEndpoint("anthropic provider credential reference is required")
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
	req.Header.Set("x-api-key", token)
	return nil
}
