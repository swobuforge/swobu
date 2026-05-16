package anthropic

import (
	"context"
	"io"
	"net/http"
	"strings"

	modelcatalogopenaicompat "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalogprotocols/openaicompat"
	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/httpedge"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	messages "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/messages"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

const (
	anthropicVersionHeaderValue = "2023-06-01"
	swobuCallerUAHeaderValue    = "swobu/dev"
)

type ProviderExecutorAdapter struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
}

func NewExecutor(client *http.Client, credentials providersruntime.CredentialProvider) ProviderExecutorAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return ProviderExecutorAdapter{
		client:      client,
		credentials: credentials,
	}
}

// NewRuntime builds a complete Anthropic provider runtime.
func NewRuntime(providerID providercatalog.ProviderID, client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntime {
	executor := NewExecutor(client, credentials)
	return providersruntime.ProviderRuntime{
		ProviderID:         providerID,
		Executor:           executor,
		CredentialProvider: credentials,
		ModelCatalogClient: executor,
	}
}

func (e ProviderExecutorAdapter) Execute(ctx context.Context, req ports.ProviderRequest) (ports.ProviderResponse, error) {
	if req.Target.ProtocolKind != protocolkind.Messages {
		return ports.ProviderResponse{}, canonical.UnsupportedOperation("anthropic provider requires messages protocol")
	}
	if req.Request == nil {
		return ports.ProviderResponse{}, canonical.BadRequest("canonical request is required")
	}
	if strings.TrimSpace(req.Target.BaseURL) == "" { // trimlowerlint:allow boundary canonicalization
		return ports.ProviderResponse{}, canonical.BadEndpoint("anthropic provider base URL is required")
	}

	wireReq, err := e.encodeRequest(req.Request, req.Contract.Streaming)
	if err != nil {
		return ports.ProviderResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, wireReq.Method, httpedge.JoinBaseURLAndPath(req.Target.BaseURL, wireReq.Path), wireReq.Body)
	if err != nil {
		return ports.ProviderResponse{}, canonical.BadEndpoint("anthropic provider request could not be built")
	}
	if wireReq.HasBody {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("anthropic-version", anthropicVersionHeaderValue)
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, req.Target.ProviderID(), req.Target.CredentialRef); err != nil {
		return ports.ProviderResponse{}, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return ports.ProviderResponse{}, canonical.BadEndpoint("anthropic provider request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
		return ports.ProviderResponse{}, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	if resp.StatusCode >= 400 {
		defer func() {
			_ = resp.Body.Close()
		}()
		return ports.ProviderResponse{}, httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
	}
	decoder, err := anthropicResponseDecoder(req.Target.ProviderID(), req.Target.ProtocolKind, req.Contract.Streaming)
	if err != nil {
		_ = resp.Body.Close()
		return ports.ProviderResponse{}, err
	}
	return decoder(resp.Body)
}

func (e ProviderExecutorAdapter) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	if strings.TrimSpace(target.BaseURL) == "" { // trimlowerlint:allow boundary canonicalization
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
	models, err := modelcatalogopenaicompat.DecodeModelIDs(resp.Body)
	if err != nil {
		return nil, canonical.InternalError("backend model catalog could not be decoded")
	}
	return models, nil
}

func (e ProviderExecutorAdapter) encodeRequest(req canonical.CanonicalRequest, deliveryMode bool) (protocols.WireRequest, error) {
	return messages.EncodeRequest(req, deliveryMode)
}

func anthropicResponseDecoder(providerIDRaw string, protocolKind protocolkind.ProtocolKind, delivery bool) (providersruntime.ResponseDecoder, error) {
	if err := providersruntime.RequireProviderAndProtocol(
		providerIDRaw,
		providercatalog.ProviderSpecAnthropic,
		protocolKind,
		protocolkind.Messages,
		"anthropic",
	); err != nil {
		return nil, err
	}
	streamingDecoder := func(body io.ReadCloser) (ports.ProviderResponse, error) {
		return ports.NewEnvelopeStreamingProviderResponse(messages.DecodeResponseStream(body, "provider_stream:anthropic_messages")), nil
	}
	bufferedDecoder := func(body io.ReadCloser) (ports.ProviderResponse, error) {
		defer func() {
			_ = body.Close()
		}()
		raw, err := io.ReadAll(body)
		if err != nil {
			return ports.ProviderResponse{}, canonical.InternalError("backend success response could not be read")
		}
		result, err := messages.DecodeResponseBuffered(raw)
		if err != nil {
			return ports.ProviderResponse{}, err
		}
		return ports.NewBufferedProviderResponse(result), nil
	}
	decoder, ok := providersruntime.SelectResponseDecoder(delivery, streamingDecoder, bufferedDecoder)
	if !ok {
		return nil, canonical.UnsupportedDelivery("anthropic provider delivery variant is not implemented")
	}
	return decoder, nil
}

func (e ProviderExecutorAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string) error {
	if strings.TrimSpace(credentialRef) == "" { // trimlowerlint:allow boundary canonicalization
		return canonical.BadEndpoint("anthropic provider credential reference is required")
	}
	if e.credentials == nil {
		return canonical.BadEndpoint("credential resolver is not configured")
	}
	token, err := e.credentials.ResolveCredential(ctx, providerSpec, credentialRef)
	if err != nil {
		return canonical.BadEndpoint("credential reference could not be resolved")
	}
	if strings.TrimSpace(token) == "" { // trimlowerlint:allow boundary canonicalization
		return canonical.BadEndpoint("credential reference resolved to an empty token")
	}
	req.Header.Set("x-api-key", token)
	return nil
}
