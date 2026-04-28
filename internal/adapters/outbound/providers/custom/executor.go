// selection, wire realization, and response decoding in one outbound seam.
package custom

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/metrofun/swobu/internal/adapters/outbound/protocols"
	"github.com/metrofun/swobu/internal/adapters/outbound/protocols/chatcompletions"
	"github.com/metrofun/swobu/internal/adapters/outbound/protocols/completions"
	"github.com/metrofun/swobu/internal/adapters/outbound/protocols/responses"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/domain/providercatalog"
	"github.com/metrofun/swobu/internal/platform/httpcontent"
	"github.com/metrofun/swobu/internal/ports"
)

type CredentialResolver interface {
	// ResolveCredential returns the provider token for one configured credential reference.
	ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error)
}

type ProviderExecutorAdapter struct {
	client      *http.Client
	credentials CredentialResolver
}

const swobuCallerUAHeaderValue = "swobu/dev"

// NewExecutor builds the custom provider wiring adapter around commodity HTTP transport.
func NewExecutor(client *http.Client, credentials CredentialResolver) ProviderExecutorAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return ProviderExecutorAdapter{
		client:      client,
		credentials: credentials,
	}
}

// Execute applies provider wiring, performs the backend HTTP call, and decodes
// successful responses into canonical semantics. Backend-origin failures remain
// backend errors rather than being normalized into Swobu success envelopes.
func (e ProviderExecutorAdapter) Execute(ctx context.Context, req ports.ExecuteRequest) (ports.ExecuteResponse, error) {
	if !supportsCustomAdapterProviderSpec(req.Target.ProviderSpecName()) {
		return ports.ExecuteResponse{}, compatibility.BadEndpoint("provider spec is unsupported by custom adapter")
	}
	return e.executeOnce(ctx, req)
}

// ListModels reads the OpenAI-compatible model catalog for one selected custom
// provider target. This is an operator-support path, not a compatibility-path
// semantic request.
func (e ProviderExecutorAdapter) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	if !supportsCustomAdapterProviderSpec(target.ProviderSpecName()) {
		return nil, compatibility.BadEndpoint("provider spec is unsupported by custom adapter")
	}
	if strings.TrimSpace(target.BaseURL) == "" {
		return nil, compatibility.BadEndpoint("custom provider base URL is required")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURL(target.BaseURL, "/models"), nil)
	if err != nil {
		return nil, compatibility.BadEndpoint("custom provider model catalog request could not be built")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, target.ProviderSpecName(), target.CredentialRef); err != nil {
		return nil, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, compatibility.BadEndpoint("custom provider model catalog request failed before backend response")
	}
	resp, err = decodeResponseContentCoding(resp)
	if err != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
		return nil, compatibility.InternalError("backend response content encoding is unsupported or invalid")
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, compatibility.NewBackendError(
			target.BackendRef,
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
			strings.TrimSpace(resp.Header.Get("Retry-After")),
		)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, compatibility.InternalError("backend model catalog could not be decoded")
	}
	models := make([]string, 0, len(payload.Data))
	for _, model := range payload.Data {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		models = append(models, id)
	}
	slices.Sort(models)
	models = slices.Compact(models)
	return models, nil
}

func (e ProviderExecutorAdapter) executeOnce(ctx context.Context, req ports.ExecuteRequest) (ports.ExecuteResponse, error) {
	if req.Request == nil {
		return ports.ExecuteResponse{}, compatibility.BadRequest("canonical request is required")
	}
	if strings.TrimSpace(req.Target.BaseURL) == "" {
		return ports.ExecuteResponse{}, compatibility.BadEndpoint("custom provider base URL is required")
	}

	wireReq, err := e.encodeRequest(req.Target, req.Request, req.Contract.DeliveryMode)
	if err != nil {
		return ports.ExecuteResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, wireReq.Method, joinURL(req.Target.BaseURL, wireReq.Path), wireReq.Body)
	if err != nil {
		return ports.ExecuteResponse{}, compatibility.BadEndpoint("custom provider request could not be built")
	}
	if wireReq.HasBody {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)

	if err := e.applyCredential(ctx, httpReq, req.Target.ProviderSpecName(), req.Target.CredentialRef); err != nil {
		return ports.ExecuteResponse{}, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return ports.ExecuteResponse{}, compatibility.BadEndpoint("custom provider request failed before backend response")
	}
	resp, err = decodeResponseContentCoding(resp)
	if err != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
		return ports.ExecuteResponse{}, compatibility.InternalError("backend response content encoding is unsupported or invalid")
	}
	if resp.StatusCode >= 400 {
		defer func() {
			_ = resp.Body.Close()
		}()
		raw, _ := io.ReadAll(resp.Body)
		backendErr := compatibility.NewBackendError(
			req.Target.BackendRef,
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
			strings.TrimSpace(resp.Header.Get("Retry-After")),
		)
		return ports.ExecuteResponse{}, classifyBackendError(backendErr)
	}

	if req.Contract.DeliveryMode == compatibility.DeliveryModeStreaming {
		return e.decodeStreamingResponse(req.Target, resp.Body)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ports.ExecuteResponse{}, compatibility.InternalError("backend success response could not be read")
	}
	return e.decodeBufferedResponse(req.Target, raw)
}

// encodeRequest selects the concrete protocol adapter from durable target intent.
func (e ProviderExecutorAdapter) encodeRequest(target ports.RoutableTarget, req compatibility.CanonicalRequest, deliveryMode compatibility.DeliveryMode) (protocols.WireRequest, error) {
	switch target.ProtocolKind {
	case protocolsurface.ChatCompletions:
		return chatcompletions.Realize(req, deliveryMode)
	case protocolsurface.Responses:
		return responses.Realize(req, deliveryMode)
	case protocolsurface.Completions:
		return completions.Realize(req, deliveryMode)
	case protocolsurface.Messages:
		return protocols.WireRequest{}, compatibility.UnsupportedOperation("custom provider does not implement the messages protocol")
	default:
		return protocols.WireRequest{}, compatibility.BadEndpoint("custom provider protocol kind is unsupported")
	}
}

// applyCredential keeps auth resolution at the provider edge so canonicals and
// app orchestration never need to know provider token mechanics.
func (e ProviderExecutorAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string) error {
	if strings.TrimSpace(credentialRef) == "" {
		return nil
	}
	if e.credentials == nil {
		return compatibility.BadEndpoint("credential resolver is not configured")
	}
	token, err := e.credentials.ResolveCredential(ctx, providerSpec, credentialRef)
	if err != nil {
		return compatibility.BadEndpoint("credential reference could not be resolved")
	}
	if strings.TrimSpace(token) == "" {
		return compatibility.BadEndpoint("credential reference resolved to an empty token")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

func joinURL(baseURL string, suffix string) string {
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(suffix, "/")
}

func decodeResponseContentCoding(resp *http.Response) (*http.Response, error) {
	contentEncoding := strings.TrimSpace(resp.Header.Get("Content-Encoding"))
	if contentEncoding == "" {
		return resp, nil
	}

	decodedBody, err := httpcontent.DecodeStream(contentEncoding, resp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body = decodedBody
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	return resp, nil
}

func supportsCustomAdapterProviderSpec(providerSpec string) bool {
	adapter, ok := providercatalog.AdapterForSpec(providerSpec)
	return ok && adapter == providercatalog.AdapterCustomOpenAICompatible
}

// decodeBufferedResponse converts backend success payloads into canonical
// buffered results before they leave provider adaptation.
func (e ProviderExecutorAdapter) decodeBufferedResponse(target ports.RoutableTarget, raw []byte) (ports.ExecuteResponse, error) {
	switch target.ProtocolKind {
	case protocolsurface.ChatCompletions:
		result, err := chatcompletions.DecodeBufferedResult(raw)
		if err != nil {
			return ports.ExecuteResponse{}, err
		}
		return ports.NewBufferedExecuteResponse(result), nil
	case protocolsurface.Responses:
		result, err := responses.DecodeBufferedResult(raw)
		if err != nil {
			return ports.ExecuteResponse{}, err
		}
		return ports.NewBufferedExecuteResponse(result), nil
	case protocolsurface.Completions:
		result, err := completions.DecodeBufferedResult(raw)
		if err != nil {
			return ports.ExecuteResponse{}, err
		}
		return ports.NewBufferedExecuteResponse(result), nil
	case protocolsurface.Messages:
		return ports.ExecuteResponse{}, compatibility.UnsupportedOperation("custom provider does not decode the messages protocol")
	default:
		return ports.ExecuteResponse{}, compatibility.BadEndpoint("custom provider protocol kind is unsupported")
	}
}

// decodeStreamingResponse converts backend success streams into canonical output-event streams.
// Provider SSE frames must not leak past this boundary as transport-shaped success semantics.
func (e ProviderExecutorAdapter) decodeStreamingResponse(target ports.RoutableTarget, body io.ReadCloser) (ports.ExecuteResponse, error) {
	switch target.ProtocolKind {
	case protocolsurface.ChatCompletions:
		return ports.NewStreamingExecuteResponse(chatcompletions.DecodeStream(body)), nil
	case protocolsurface.Responses:
		return ports.NewStreamingExecuteResponse(responses.DecodeStream(body)), nil
	case protocolsurface.Completions:
		return ports.NewStreamingExecuteResponse(completions.DecodeStream(body)), nil
	case protocolsurface.Messages:
		_ = body.Close()
		return ports.ExecuteResponse{}, compatibility.UnsupportedOperation("custom provider does not decode messages protocol streams")
	default:
		_ = body.Close()
		return ports.ExecuteResponse{}, compatibility.BadEndpoint("custom provider protocol kind is unsupported")
	}
}
