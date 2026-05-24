// selection, wire realization, and response decoding in one outbound seam.
package openaicompat

import (
	"context"
	"io"
	"net/http"
	"strings"

	modelcatalogopenaicompat "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalogprotocols/openaicompat"
	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	chatcompletions "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/chat_completions"
	completions "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/completions"
	responses "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/responses"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/httpedge"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

type ProviderExecutorAdapter struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
}

type protocolCodecDispatchSpec struct {
	realize        func(req canonical.CanonicalRequest, streaming bool) (protocols.WireRequest, error)
	decodeBuffered func(raw []byte) (ports.ProviderResponse, error)
	decodeStream   func(body io.ReadCloser) (ports.ProviderResponse, error)
}

const swobuCallerUAHeaderValue = "swobu/dev"

var protocolDispatchTable = map[protocolkind.ProtocolKind]protocolCodecDispatchSpec{
	protocolkind.ChatCompletions: {
		realize: func(req canonical.CanonicalRequest, streaming bool) (protocols.WireRequest, error) {
			return chatcompletions.EncodeRequest(req, streaming)
		},
		decodeBuffered: func(raw []byte) (ports.ProviderResponse, error) {
			result, err := chatcompletions.DecodeResponseBuffered(raw)
			if err != nil {
				return ports.ProviderResponse{}, err
			}
			return ports.NewBufferedProviderResponse(result), nil
		},
		decodeStream: func(body io.ReadCloser) (ports.ProviderResponse, error) {
			return ports.NewEnvelopeStreamingProviderResponse(chatcompletions.DecodeResponseStream(body, "provider_stream:chat_completions")), nil
		},
	},
	protocolkind.Responses: {
		realize: func(req canonical.CanonicalRequest, streaming bool) (protocols.WireRequest, error) {
			return responses.EncodeRequest(req, streaming)
		},
		decodeBuffered: func(raw []byte) (ports.ProviderResponse, error) {
			result, err := responses.DecodeResponseBuffered(raw)
			if err != nil {
				return ports.ProviderResponse{}, err
			}
			return ports.NewBufferedProviderResponse(result), nil
		},
		decodeStream: func(body io.ReadCloser) (ports.ProviderResponse, error) {
			return ports.NewEnvelopeStreamingProviderResponse(responses.DecodeResponseStream(body, "provider_stream:responses")), nil
		},
	},
	protocolkind.Completions: {
		realize: func(req canonical.CanonicalRequest, streaming bool) (protocols.WireRequest, error) {
			return completions.EncodeRequest(req, streaming)
		},
		decodeBuffered: func(raw []byte) (ports.ProviderResponse, error) {
			result, err := completions.DecodeResponseBuffered(raw)
			if err != nil {
				return ports.ProviderResponse{}, err
			}
			return ports.NewBufferedProviderResponse(result), nil
		},
		decodeStream: func(body io.ReadCloser) (ports.ProviderResponse, error) {
			return ports.NewEnvelopeStreamingProviderResponse(completions.DecodeResponseStream(body, "provider_stream:completions")), nil
		},
	},
}

// NewExecutor builds the OpenAI-compatible provider wiring adapter around commodity HTTP transport.
func NewExecutor(client *http.Client, credentials providersruntime.CredentialProvider) ProviderExecutorAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return ProviderExecutorAdapter{
		client:      client,
		credentials: credentials,
	}
}

// NewRuntime builds a complete OpenAI-compatible provider runtime.
func NewRuntime(providerID providercatalog.ProviderID, client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	executor := NewExecutor(client, credentials)
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         providerID,
		Executor:           executor,
		CredentialProvider: credentials,
		ModelCatalogClient: executor,
	}
}

// Execute applies provider wiring, performs the backend HTTP call, and decodes
// successful responses into canonical semantics. Backend-origin failures remain
// backend errors rather than being normalized into Swobu success envelopes.
// ListModels reads the OpenAI-compatible model catalog for one selected OpenAI-compatible
// provider target. This is an operator-support path, not a compatibility-path
// semantic request.
func (e ProviderExecutorAdapter) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	if strings.TrimSpace(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("OpenAI-compatible provider base URL is required")
	}
	if requiresExplicitCredentialRef(target.ProviderID(), target.BaseURL, target.CredentialRef) {
		return nil, canonical.BadEndpoint(providerCredentialRequiredMessage(target.ProviderID()))
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, httpedge.JoinBaseURLAndPath(target.BaseURL, "/models"), nil)
	if err != nil {
		return nil, canonical.BadEndpoint("OpenAI-compatible provider model catalog request could not be built")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, target.ProviderID(), target.CredentialRef); err != nil {
		return nil, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("OpenAI-compatible provider model catalog request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
		return nil, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		return nil, httpedge.ReadBackendHTTPError(resp, target.BackendRef)
	}
	models, err := modelcatalogopenaicompat.DecodeModelIDs(resp.Body)
	if err != nil {
		return nil, canonical.InternalError("backend model catalog could not be decoded")
	}
	return models, nil
}

func (e ProviderExecutorAdapter) ValidateCredentials(ctx context.Context, target ports.RoutableTarget) error {
	_, err := e.ListModels(ctx, target)
	return err
}

func (e ProviderExecutorAdapter) Execute(ctx context.Context, req ports.ProviderRequest) (ports.ProviderResponse, error) {
	if req.Request == nil {
		return ports.ProviderResponse{}, canonical.BadRequest("canonical request is required")
	}
	if strings.TrimSpace(req.Target.BaseURL) == "" { // swobu:io-string source=boundary
		return ports.ProviderResponse{}, canonical.BadEndpoint("OpenAI-compatible provider base URL is required")
	}
	if requiresExplicitCredentialRef(req.Target.ProviderID(), req.Target.BaseURL, req.Target.CredentialRef) {
		return ports.ProviderResponse{}, canonical.BadEndpoint(providerCredentialRequiredMessage(req.Target.ProviderID()))
	}

	dispatch, streaming, err := resolveProviderProtocolDispatch(req.Target)
	if err != nil {
		return ports.ProviderResponse{}, err
	}

	wireReq, err := dispatch.realize(req.Request, streaming)
	if err != nil {
		return ports.ProviderResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, wireReq.Method, httpedge.JoinBaseURLAndPath(req.Target.BaseURL, wireReq.Path), wireReq.Body)
	if err != nil {
		return ports.ProviderResponse{}, canonical.BadEndpoint("OpenAI-compatible provider request could not be built")
	}
	if wireReq.HasBody {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)

	if err := e.applyCredential(ctx, httpReq, req.Target.ProviderID(), req.Target.CredentialRef); err != nil {
		return ports.ProviderResponse{}, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return ports.ProviderResponse{}, canonical.BadEndpoint("OpenAI-compatible provider request failed before backend response")
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
		backendErr := httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
		return ports.ProviderResponse{}, classifyBackendError(backendErr)
	}

	if streaming {
		if dispatch.decodeStream == nil {
			_ = resp.Body.Close()
			return ports.ProviderResponse{}, canonical.UnsupportedDelivery("OpenAI-compatible provider streaming delivery is not implemented")
		}
		return dispatch.decodeStream(resp.Body)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ports.ProviderResponse{}, canonical.InternalError("backend success response could not be read")
	}
	if dispatch.decodeBuffered == nil {
		return ports.ProviderResponse{}, canonical.UnsupportedDelivery("OpenAI-compatible provider buffered delivery is not implemented")
	}
	return dispatch.decodeBuffered(raw)
}

// applyCredential keeps auth resolution at the provider edge so canonicals and
// app orchestration never need to know provider token mechanics.
func (e ProviderExecutorAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string) error {
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
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

func resolveProviderProtocolDispatch(target ports.RoutableTarget) (protocolCodecDispatchSpec, bool, error) {
	providerProtocol := strings.TrimSpace(target.ProviderProtocol) // swobu:io-string source=boundary
	if providerProtocol == "" || providerProtocol == providercatalog.ProviderProtocolAuto {
		return protocolCodecDispatchSpec{}, false, canonical.BadEndpoint("OpenAI-compatible provider protocol must be concrete")
	}
	kind, _, ok := providercatalog.ProviderProtocolKindAndFrame(target.ProviderID(), providerProtocol)
	if !ok {
		return protocolCodecDispatchSpec{}, false, canonical.BadEndpoint("selected provider protocol is unsupported")
	}
	dispatch, err := protocolCodecDispatchFor(target.ProviderID(), kind)
	if err != nil {
		return protocolCodecDispatchSpec{}, false, err
	}
	streaming := strings.HasSuffix(providerProtocol, "_stream")
	return dispatch, streaming, nil
}

func protocolCodecDispatchFor(providerIDRaw string, kind protocolkind.ProtocolKind) (protocolCodecDispatchSpec, error) {
	providerID, ok := providercatalog.ParseProviderID(strings.TrimSpace(providerIDRaw)) // swobu:io-string source=boundary
	if !ok {
		return protocolCodecDispatchSpec{}, canonical.BadEndpoint("provider id is unsupported for OpenAI-compatible adapter runtime")
	}
	switch providerID {
	case providercatalog.ProviderSpecOpenAI, providercatalog.ProviderSpecOpenRouter, providercatalog.ProviderSpecOpenAICompatible, providercatalog.ProviderSpecOllama:
	default:
		return protocolCodecDispatchSpec{}, canonical.BadEndpoint("provider id is unsupported for OpenAI-compatible adapter runtime")
	}
	dispatch, ok := protocolDispatchTable[kind]
	if !ok {
		if kind == protocolkind.Messages {
			return protocolCodecDispatchSpec{}, canonical.UnsupportedOperation("OpenAI-compatible provider does not implement the messages protocol")
		}
		return protocolCodecDispatchSpec{}, canonical.UnsupportedOperation("OpenAI-compatible provider protocol kind is not implemented")
	}
	return dispatch, nil
}

func requiresExplicitCredentialRef(providerSpec string, baseURL string, credentialRef string) bool {
	if strings.TrimSpace(credentialRef) != "" { // swobu:io-string source=boundary
		return false
	}
	return providercatalog.RequiresCredential(strings.TrimSpace(providerSpec), strings.TrimSpace(baseURL)) // swobu:io-string source=boundary
}

func providerCredentialRequiredMessage(providerSpec string) string {
	providerID, ok := providercatalog.ParseProviderID(strings.TrimSpace(providerSpec)) // swobu:io-string source=boundary
	if !ok {
		return "provider credential reference is required"
	}
	return string(providerID) + " provider credential reference is required"
}
