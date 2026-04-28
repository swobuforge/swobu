package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/metrofun/swobu/internal/adapters/outbound/protocols"
	"github.com/metrofun/swobu/internal/adapters/outbound/protocols/messages"
	"github.com/metrofun/swobu/internal/domain/compatibility"
	"github.com/metrofun/swobu/internal/domain/protocolsurface"
	"github.com/metrofun/swobu/internal/domain/providercatalog"
	"github.com/metrofun/swobu/internal/platform/httpcontent"
	"github.com/metrofun/swobu/internal/ports"
)

const (
	anthropicVersionHeaderValue = "2023-06-01"
	swobuCallerUAHeaderValue    = "swobu/dev"
)

type CredentialResolver interface {
	ResolveCredential(ctx context.Context, providerSpec string, credentialRef string) (string, error)
}

type ProviderExecutorAdapter struct {
	client      *http.Client
	credentials CredentialResolver
}

func NewExecutor(client *http.Client, credentials CredentialResolver) ProviderExecutorAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return ProviderExecutorAdapter{
		client:      client,
		credentials: credentials,
	}
}

func (e ProviderExecutorAdapter) Execute(ctx context.Context, req ports.ExecuteRequest) (ports.ExecuteResponse, error) {
	if !isAnthropicProviderSpec(req.Target.ProviderSpecName()) {
		return ports.ExecuteResponse{}, compatibility.BadEndpoint("provider spec is unsupported by anthropic adapter")
	}
	if req.Target.ProtocolKind != protocolsurface.Messages {
		return ports.ExecuteResponse{}, compatibility.UnsupportedOperation("anthropic provider requires messages protocol")
	}
	if req.Request == nil {
		return ports.ExecuteResponse{}, compatibility.BadRequest("canonical request is required")
	}
	if strings.TrimSpace(req.Target.BaseURL) == "" {
		return ports.ExecuteResponse{}, compatibility.BadEndpoint("anthropic provider base URL is required")
	}

	wireReq, err := e.encodeRequest(req.Request, req.Contract.DeliveryMode)
	if err != nil {
		return ports.ExecuteResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, wireReq.Method, joinURL(req.Target.BaseURL, wireReq.Path), wireReq.Body)
	if err != nil {
		return ports.ExecuteResponse{}, compatibility.BadEndpoint("anthropic provider request could not be built")
	}
	if wireReq.HasBody {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("anthropic-version", anthropicVersionHeaderValue)
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, req.Target.ProviderSpecName(), req.Target.CredentialRef); err != nil {
		return ports.ExecuteResponse{}, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return ports.ExecuteResponse{}, compatibility.BadEndpoint("anthropic provider request failed before backend response")
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
		return ports.ExecuteResponse{}, compatibility.NewBackendError(
			req.Target.BackendRef,
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
			strings.TrimSpace(resp.Header.Get("Retry-After")),
		)
	}
	if req.Contract.DeliveryMode == compatibility.DeliveryModeStreaming {
		return ports.NewStreamingExecuteResponse(messages.DecodeStream(resp.Body)), nil
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ports.ExecuteResponse{}, compatibility.InternalError("backend success response could not be read")
	}
	result, err := messages.DecodeBufferedResult(raw)
	if err != nil {
		return ports.ExecuteResponse{}, err
	}
	return ports.NewBufferedExecuteResponse(result), nil
}

func (e ProviderExecutorAdapter) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	if !isAnthropicProviderSpec(target.ProviderSpecName()) {
		return nil, compatibility.BadEndpoint("provider spec is unsupported by anthropic adapter")
	}
	if strings.TrimSpace(target.BaseURL) == "" {
		return nil, compatibility.BadEndpoint("anthropic provider base URL is required")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURL(target.BaseURL, "/models"), nil)
	if err != nil {
		return nil, compatibility.BadEndpoint("anthropic provider model catalog request could not be built")
	}
	httpReq.Header.Set("anthropic-version", anthropicVersionHeaderValue)
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, target.ProviderSpecName(), target.CredentialRef); err != nil {
		return nil, err
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, compatibility.BadEndpoint("anthropic provider model catalog request failed before backend response")
	}
	resp, err = decodeResponseContentCoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return nil, compatibility.InternalError("backend response content encoding is unsupported or invalid")
	}
	defer func() { _ = resp.Body.Close() }()
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

func (e ProviderExecutorAdapter) encodeRequest(req compatibility.CanonicalRequest, deliveryMode compatibility.DeliveryMode) (protocols.WireRequest, error) {
	return messages.Realize(req, deliveryMode)
}

func isAnthropicProviderSpec(providerSpec string) bool {
	adapter, ok := providercatalog.AdapterForSpec(providerSpec)
	return ok && adapter == providercatalog.AdapterAnthropicMessages
}

func (e ProviderExecutorAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string) error {
	if strings.TrimSpace(credentialRef) == "" {
		return compatibility.BadEndpoint("anthropic provider credential reference is required")
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
	req.Header.Set("x-api-key", token)
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
