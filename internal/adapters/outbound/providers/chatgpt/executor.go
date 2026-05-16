package chatgpt

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols/codex"
	responses "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/responses"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/httpedge"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

const (
	swobuCallerUAHeaderValue = "swobu/dev"
	chatGPTCodexExecuteBase  = "https://chatgpt.com/backend-api/codex"
	chatGPTSubagentHeaderKey = "x-openai-subagent"
	chatGPTSubagentHeaderVal = "swobu"
)

var catalogBaseURL = "https://swobu.com"

type ProviderExecutorAdapter struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
	catalogBase string
}

func NewExecutor(client *http.Client, credentials providersruntime.CredentialProvider) ProviderExecutorAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return ProviderExecutorAdapter{
		client:      client,
		credentials: credentials,
		catalogBase: catalogBaseURL,
	}
}

// NewRuntime builds a complete ChatGPT provider runtime.
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
	if req.Request == nil {
		return ports.ProviderResponse{}, canonical.BadRequest("canonical request is required")
	}
	wireReq, err := codex.EncodeRequest(req.Request, req.Contract.Streaming)
	if err != nil {
		return ports.ProviderResponse{}, err
	}
	baseURL := resolveChatGPTExecuteBaseURL(req.Target.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, wireReq.Method, httpedge.JoinBaseURLAndPath(baseURL, wireReq.Path), wireReq.Body)
	if err != nil {
		return ports.ProviderResponse{}, canonical.BadEndpoint("chatgpt provider request could not be built")
	}
	if wireReq.HasBody {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	httpReq.Header.Set(chatGPTSubagentHeaderKey, chatGPTSubagentHeaderVal)
	if err := e.applyCredential(ctx, httpReq, req.Target.ProviderID(), req.Target.CredentialRef); err != nil {
		return ports.ProviderResponse{}, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return ports.ProviderResponse{}, canonical.BadEndpoint("chatgpt provider request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return ports.ProviderResponse{}, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		return ports.ProviderResponse{}, httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
	}
	decoder, err := chatGPTResponseDecoder(req.Target.ProviderID(), protocolkind.Responses, req.Contract.Streaming)
	if err != nil {
		_ = resp.Body.Close()
		return ports.ProviderResponse{}, err
	}
	return decoder(resp.Body)
}

func (e ProviderExecutorAdapter) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	tier, ok := resolveChatGPTSubscriptionTier(target.CredentialRef)
	if !ok {
		return nil, canonical.BadEndpoint("chatgpt subscription tier could not be resolved from credential")
	}
	resourceURL := strings.TrimRight(e.catalogBase, "/") + "/api/v1/model-catalog/chatgpt/subscriptions/" + tier
	slog.Debug("chatgpt model catalog request",
		"backend_ref", strings.TrimSpace(target.BackendRef), // trimlowerlint:allow boundary canonicalization
		"catalog_url", resourceURL,
		"tier", tier,
		"tier_from_credential_ref", ok,
	)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, resourceURL, nil)
	if err != nil {
		slog.Warn("chatgpt model catalog request build failed",
			"catalog_url", resourceURL,
			"error", err.Error(),
		)
		return nil, canonical.BadEndpoint("chatgpt model catalog request could not be built")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	resp, err := e.client.Do(httpReq)
	if err != nil {
		slog.Warn("chatgpt model catalog request failed",
			"catalog_url", resourceURL,
			"error", err.Error(),
		)
		return nil, canonical.BadEndpoint("chatgpt model catalog request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		slog.Warn("chatgpt model catalog content decode failed",
			"catalog_url", resourceURL,
			"error", err.Error(),
		)
		return nil, canonical.InternalError("chatgpt model catalog response content encoding is unsupported or invalid")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		slog.Warn("chatgpt model catalog backend error",
			"catalog_url", resourceURL,
			"status_code", resp.StatusCode,
			"tier", tier,
		)
		return nil, httpedge.ReadBackendHTTPError(resp, target.BackendRef)
	}
	var payload struct {
		ModelIDs []string `json:"model_ids"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		slog.Warn("chatgpt model catalog decode failed",
			"catalog_url", resourceURL,
			"error", err.Error(),
		)
		return nil, canonical.InternalError("chatgpt model catalog could not be decoded")
	}
	models := ports.CloneModelIDs(payload.ModelIDs)
	slices.Sort(models)
	models = slices.Compact(models)
	return models, nil
}

func resolveChatGPTSubscriptionTier(credentialRef string) (string, bool) {
	ref := strings.ToLower(strings.TrimSpace(credentialRef)) // trimlowerlint:allow boundary canonicalization
	for _, tier := range []string{"team", "pro", "plus", "free"} {
		if strings.Contains(ref, "/"+tier) || strings.Contains(ref, ":"+tier) || strings.Contains(ref, "_"+tier) || strings.Contains(ref, "-"+tier) {
			return tier, true
		}
	}
	if raw, err := url.QueryUnescape(ref); err == nil {
		for _, tier := range []string{"team", "pro", "plus", "free"} {
			if strings.Contains(raw, "tier="+tier) {
				return tier, true
			}
		}
	}
	return "", false
}

func resolveChatGPTExecuteBaseURL(raw string) string {
	base := strings.TrimSpace(raw) // trimlowerlint:allow boundary canonicalization
	if base == "" {
		return chatGPTCodexExecuteBase
	}
	lower := strings.ToLower(base) // trimlowerlint:allow boundary canonicalization
	if strings.Contains(lower, "chatgpt.com/backend-api/codex") {
		return strings.TrimRight(base, "/")
	}
	if strings.Contains(lower, "api.openai.com/v1") {
		return chatGPTCodexExecuteBase
	}
	return strings.TrimRight(base, "/")
}

func (e ProviderExecutorAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string) error {
	if strings.TrimSpace(credentialRef) == "" { // trimlowerlint:allow boundary canonicalization
		return canonical.BadEndpoint("chatgpt provider credential reference is required")
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

func chatGPTResponseDecoder(providerIDRaw string, protocolKind protocolkind.ProtocolKind, delivery bool) (providersruntime.ResponseDecoder, error) {
	if err := providersruntime.RequireProviderAndProtocol(
		providerIDRaw,
		providercatalog.ProviderSpecChatGPT,
		protocolKind,
		protocolkind.Responses,
		"chatgpt",
	); err != nil {
		return nil, err
	}
	streamingDecoder := func(body io.ReadCloser) (ports.ProviderResponse, error) {
		return ports.NewEnvelopeStreamingProviderResponse(responses.DecodeResponseStream(body, "provider_stream:chatgpt_responses")), nil
	}
	bufferedDecoder := func(body io.ReadCloser) (ports.ProviderResponse, error) {
		defer func() { _ = body.Close() }()
		raw, err := io.ReadAll(body)
		if err != nil {
			return ports.ProviderResponse{}, canonical.InternalError("backend success response could not be read")
		}
		result, err := responses.DecodeResponseBuffered(raw)
		if err != nil {
			return ports.ProviderResponse{}, err
		}
		return ports.NewBufferedProviderResponse(result), nil
	}
	decoder, ok := providersruntime.SelectResponseDecoder(delivery, streamingDecoder, bufferedDecoder)
	if !ok {
		return nil, canonical.UnsupportedDelivery("chatgpt provider delivery variant is not implemented")
	}
	return decoder, nil
}
