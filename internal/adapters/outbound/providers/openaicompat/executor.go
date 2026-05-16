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

type protocolCodecDispatch struct {
	realize        func(req canonical.CanonicalRequest, streaming bool) (protocols.WireRequest, error)
	decodeBuffered func(raw []byte) (ports.ProviderResponse, error)
	decodeStream   func(body io.ReadCloser) (ports.ProviderResponse, error)
}

const swobuCallerUAHeaderValue = "swobu/dev"

var protocolDispatchTable = map[protocolkind.ProtocolKind]protocolCodecDispatch{
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
func NewRuntime(providerID providercatalog.ProviderID, client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntime {
	executor := NewExecutor(client, credentials)
	return providersruntime.ProviderRuntime{
		ProviderID:         providerID,
		Executor:           executor,
		CredentialProvider: credentials,
		ModelCatalogClient: executor,
	}
}

// Execute applies provider wiring, performs the backend HTTP call, and decodes
// successful responses into canonical semantics. Backend-origin failures remain
// backend errors rather than being normalized into Swobu success envelopes.
func (e ProviderExecutorAdapter) Execute(ctx context.Context, req ports.ProviderRequest) (ports.ProviderResponse, error) {
	return e.executeOnce(ctx, req)
}

// ListModels reads the OpenAI-compatible model catalog for one selected OpenAI-compatible
// provider target. This is an operator-support path, not a compatibility-path
// semantic request.
func (e ProviderExecutorAdapter) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	if strings.TrimSpace(target.BaseURL) == "" { // trimlowerlint:allow boundary canonicalization
		return nil, canonical.BadEndpoint("OpenAI-compatible provider base URL is required")
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

func (e ProviderExecutorAdapter) executeOnce(ctx context.Context, req ports.ProviderRequest) (ports.ProviderResponse, error) {
	if req.Request == nil {
		return ports.ProviderResponse{}, canonical.BadRequest("canonical request is required")
	}
	if strings.TrimSpace(req.Target.BaseURL) == "" { // trimlowerlint:allow boundary canonicalization
		return ports.ProviderResponse{}, canonical.BadEndpoint("OpenAI-compatible provider base URL is required")
	}

	wireReq, err := e.encodeRequest(req.Target, req.Request, req.Contract.ProviderCallMode.Streaming())
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

	if req.Contract.ProviderCallMode.Streaming() {
		return e.decodeStreamingResponse(req.Target, resp.Body)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ports.ProviderResponse{}, canonical.InternalError("backend success response could not be read")
	}
	return e.decodeBufferedResponse(req.Target, raw)
}

func (e ProviderExecutorAdapter) encodeRequest(target ports.RoutableTarget, req canonical.CanonicalRequest, deliveryMode bool) (protocols.WireRequest, error) {
	dispatch, err := protocolCodecDispatchFor(target.ProviderID(), target.ProtocolKind)
	if err != nil {
		return protocols.WireRequest{}, err
	}
	return dispatch.realize(req, deliveryMode)
}

// applyCredential keeps auth resolution at the provider edge so canonicals and
// app orchestration never need to know provider token mechanics.
func (e ProviderExecutorAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string) error {
	if strings.TrimSpace(credentialRef) == "" { // trimlowerlint:allow boundary canonicalization
		return nil
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
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

// decodeBufferedResponse converts backend success payloads into canonical
// buffered results before they leave provider adaptation.
func (e ProviderExecutorAdapter) decodeBufferedResponse(target ports.RoutableTarget, raw []byte) (ports.ProviderResponse, error) {
	dispatch, err := protocolCodecDispatchFor(target.ProviderID(), target.ProtocolKind)
	if err != nil {
		return ports.ProviderResponse{}, err
	}
	if dispatch.decodeBuffered == nil {
		return ports.ProviderResponse{}, canonical.UnsupportedDelivery("OpenAI-compatible provider buffered delivery is not implemented")
	}
	return dispatch.decodeBuffered(raw)
}

// decodeStreamingResponse converts backend success streams into canonical output-event streams.
// Provider SSE frames must not leak past this boundary as transport-shaped success semantics.
func (e ProviderExecutorAdapter) decodeStreamingResponse(target ports.RoutableTarget, body io.ReadCloser) (ports.ProviderResponse, error) {
	dispatch, err := protocolCodecDispatchFor(target.ProviderID(), target.ProtocolKind)
	if err != nil {
		_ = body.Close()
		return ports.ProviderResponse{}, err
	}
	if dispatch.decodeStream == nil {
		_ = body.Close()
		return ports.ProviderResponse{}, canonical.UnsupportedDelivery("OpenAI-compatible provider streaming delivery is not implemented")
	}
	return dispatch.decodeStream(body)
}

func protocolCodecDispatchFor(providerIDRaw string, kind protocolkind.ProtocolKind) (protocolCodecDispatch, error) {
	providerID, ok := providercatalog.ParseProviderID(strings.TrimSpace(providerIDRaw)) // trimlowerlint:allow boundary canonicalization
	if !ok {
		return protocolCodecDispatch{}, canonical.BadEndpoint("provider id is unsupported for OpenAI-compatible adapter runtime")
	}
	switch providerID {
	case providercatalog.ProviderSpecOpenAI, providercatalog.ProviderSpecOpenRouter, providercatalog.ProviderSpecOpenAICompatible, providercatalog.ProviderSpecOllama:
	default:
		return protocolCodecDispatch{}, canonical.BadEndpoint("provider id is unsupported for OpenAI-compatible adapter runtime")
	}
	dispatch, ok := protocolDispatchTable[kind]
	if !ok {
		if kind == protocolkind.Messages {
			return protocolCodecDispatch{}, canonical.UnsupportedOperation("OpenAI-compatible provider does not implement the messages protocol")
		}
		return protocolCodecDispatch{}, canonical.UnsupportedOperation("OpenAI-compatible provider protocol kind is not implemented")
	}
	return dispatch, nil
}
