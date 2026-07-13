package anthropic

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	providercompat "github.com/swobuforge/swobu/internal/adapters/outbound/providers/providercompat"
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
		ProviderExecutor:   executor,
		CredentialProvider: credentials,
		Discovery:          executor,
	}
}

func (e ProviderIngressResolverAdapter) ResolveProviderIngress(ctx context.Context, req ports.ProviderRequest) (ports.ProviderIngress, error) {
	if err := validateAnthropicProviderProtocol(req.Target.ProviderProtocol); err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Request.Model()) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadRequest("canonical request is required")
	}
	if strings.TrimSpace(req.Target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("anthropic provider base URL is required")
	}

	resolvedDelivery := req.Contract.ProviderDelivery
	if err := resolvedDelivery.Validate(); err != nil {
		return nil, canonical.UnsupportedDelivery("anthropic provider delivery is unsupported")
	}
	if resolvedDelivery.IsStreaming() && resolvedDelivery.Framing != delivery.FramingSSE {
		return nil, canonical.UnsupportedDelivery("anthropic provider does not implement the requested delivery framing")
	}
	if err := providercompat.EmitToolSchemaStrictDecision(ctx, req.EffectSink, req.ExchangeID, req.Target.ProviderID(), req.Target.ProtocolKind, req.Request.Tools(), false); err != nil {
		return nil, err
	}
	wireReqResult, err := messages.ProviderRequestDocumentEncoder{}.EncodeProviderRequestDocument(req.Request, resolvedDelivery, req.ExchangeID)
	if commitErr := providercompat.CommitEffects(ctx, req.EffectSink, req.ExchangeID, wireReqResult.Effects); commitErr != nil {
		return nil, commitErr
	}
	if err != nil {
		return nil, err
	}
	wireReq := wireReqResult.Value
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
	if resolvedDelivery.IsStreaming() {
		return carrier.WireStream{
			Stage:   carrier.StageProviderIngressIn,
			Family:  protocolkind.Messages,
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
		protocolkind.Messages,
		"application/json",
		resp.Header.Clone(),
		raw,
		carrier.Meta{},
	), nil
}

func (e ProviderIngressResolverAdapter) ListDeployments(ctx context.Context, target exchange.RoutableTarget) ([]ports.ProviderDeploymentRecord, error) {
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
	supportedProtocols := profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecAnthropic))
	out := make([]ports.ProviderDeploymentRecord, 0, len(models))
	for _, modelID := range models {
		out = append(out, ports.NewProviderDeployment(
			modelID,
			modelID,
			string(profile.ProviderSpecAnthropic),
			"",
			string(profile.ProviderSpecAnthropic),
			supportedProtocols,
			"",
		))
	}
	return out, nil
}

func (e ProviderIngressResolverAdapter) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	_, err := e.ListDeployments(ctx, target)
	return err
}

func validateAnthropicProviderProtocol(providerProtocol string) error {
	providerProtocol = strings.TrimSpace(providerProtocol) // swobu:io-string source=boundary
	if providerProtocol == "" || providerProtocol == profile.ProviderProtocolAuto {
		return canonical.BadEndpoint("anthropic provider protocol must be concrete")
	}
	if !profile.SupportsProviderProtocolForSpec(string(profile.ProviderSpecAnthropic), providerProtocol) {
		return canonical.BadEndpoint("selected provider protocol is unsupported for anthropic")
	}
	return nil
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
