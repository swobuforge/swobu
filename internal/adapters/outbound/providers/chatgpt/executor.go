package chatgpt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	outboundcredentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/chatgpt/codexwire"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	protocolregistry "github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
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
	tokenRefreshSkew         = 5 * time.Minute
)

var chatGPTRefreshTokenURL = "https://auth.openai.com/oauth/token"
var chatGPTOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

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

// NewRuntime builds a complete ChatGPT provider runtime.
func NewRuntime(providerID providercatalog.ProviderID, client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	executor := NewExecutor(client, credentials)
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         providerID,
		Executor:           executor,
		CredentialProvider: credentials,
		ModelCatalogClient: executor,
	}
}

func (e ProviderExecutorAdapter) Execute(ctx context.Context, req ports.ProviderRequest) (ports.ProviderResponse, error) {
	if strings.TrimSpace(req.Request.Model()) == "" {
		return ports.ProviderResponse{}, canonical.BadRequest("canonical request is required")
	}
	streaming, err := resolveChatGPTProviderProtocol(req.Target.ProviderProtocol)
	if err != nil {
		return ports.ProviderResponse{}, err
	}
	wireReq, err := codexwire.EncodeRequest(req.Request, streaming)
	if err != nil {
		return ports.ProviderResponse{}, err
	}
	baseURL := resolveChatGPTExecuteBaseURL(req.Target.BaseURL)
	var bodyBytes []byte
	if wireReq.Body != nil {
		raw, readErr := io.ReadAll(wireReq.Body)
		if readErr != nil {
			return ports.ProviderResponse{}, canonical.InternalError("provider request body could not be read")
		}
		bodyBytes = raw
	}
	newRequest := func(token string) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, wireReq.Method, httpedge.JoinBaseURLAndPath(baseURL, wireReq.Path), bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		if wireReq.HasBody {
			httpReq.Header.Set("Content-Type", "application/json")
		}
		httpReq.Header.Set("Accept", "application/json")
		httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
		httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
		httpReq.Header.Set(chatGPTSubagentHeaderKey, chatGPTSubagentHeaderVal)
		httpReq.Header.Set("Authorization", "Bearer "+token)
		return httpReq, nil
	}
	token, err := e.resolveAccessToken(ctx, req.Target.ProviderID(), req.Target.CredentialRef, false)
	if err != nil {
		return ports.ProviderResponse{}, err
	}
	httpReq, err := newRequest(token)
	if err != nil {
		return ports.ProviderResponse{}, canonical.BadEndpoint("chatgpt provider request could not be built")
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
	if resp.StatusCode == http.StatusUnauthorized {
		backendErr := httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
		recoveredToken, refreshErr := e.resolveAccessToken(ctx, req.Target.ProviderID(), req.Target.CredentialRef, true)
		if refreshErr != nil || strings.TrimSpace(recoveredToken) == "" { // swobu:io-string source=boundary
			return ports.ProviderResponse{}, backendErr
		}
		retryReq, buildErr := newRequest(recoveredToken)
		if buildErr != nil {
			return ports.ProviderResponse{}, canonical.BadEndpoint("chatgpt provider request could not be built")
		}
		retryResp, retryErr := e.client.Do(retryReq)
		if retryErr != nil {
			return ports.ProviderResponse{}, canonical.BadEndpoint("chatgpt provider request failed before backend response")
		}
		retryResp, retryErr = httpedge.DecodeHTTPResponseContentEncoding(retryResp)
		if retryErr != nil {
			defer func() { _ = retryResp.Body.Close() }()
			return ports.ProviderResponse{}, canonical.InternalError("backend response content encoding is unsupported or invalid")
		}
		resp = retryResp
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		return ports.ProviderResponse{}, httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
	}
	codec, err := protocolregistry.ForProtocolKind(protocolkind.Responses)
	if err != nil {
		_ = resp.Body.Close()
		return ports.ProviderResponse{}, err
	}
	decoder, err := chatGPTResponseDecoder(req.Target.ProviderID(), streaming, codec)
	if err != nil {
		_ = resp.Body.Close()
		return ports.ProviderResponse{}, err
	}
	return decoder(resp.Body)
}

func (e ProviderExecutorAdapter) resolveAccessToken(ctx context.Context, providerSpec string, credentialRef string, forceRefresh bool) (string, error) {
	if strings.TrimSpace(credentialRef) == "" { // swobu:io-string source=boundary
		return "", canonical.BadEndpoint("chatgpt provider credential reference is required")
	}
	if e.credentials == nil {
		return "", canonical.BadEndpoint("credential resolver is not configured")
	}
	if !forceRefresh {
		token, err := e.credentials.ResolveCredential(ctx, providerSpec, credentialRef)
		if err != nil {
			return "", canonical.BadEndpoint("credential reference could not be resolved")
		}
		if strings.TrimSpace(token) == "" { // swobu:io-string source=boundary
			return "", canonical.BadEndpoint("credential reference resolved to an empty token")
		}
		return token, nil
	}
	if err := e.refreshCredentialBundle(ctx, providerSpec, credentialRef); err != nil {
		return "", err
	}
	token, err := e.credentials.ResolveCredential(ctx, providerSpec, credentialRef)
	if err != nil {
		return "", canonical.BadEndpoint("credential reference could not be resolved")
	}
	if strings.TrimSpace(token) == "" { // swobu:io-string source=boundary
		return "", canonical.BadEndpoint("credential reference resolved to an empty token")
	}
	return token, nil
}

func (e ProviderExecutorAdapter) refreshCredentialBundle(ctx context.Context, providerSpec string, credentialRef string) error {
	raw, err := outboundcredentials.ResolveStoredSecretByRef(providerSpec, credentialRef)
	if err != nil {
		return err
	}
	bundle, isBundle, err := outboundcredentials.DecodeTokenBundle(raw)
	if err != nil || !isBundle {
		return fmt.Errorf("credential is not refreshable")
	}
	if strings.TrimSpace(bundle.RefreshToken) == "" { // swobu:io-string source=boundary
		return fmt.Errorf("credential is not refreshable")
	}
	if !bundle.ExpiresAt.IsZero() && bundle.ExpiresAt.After(time.Now().UTC().Add(tokenRefreshSkew)) && strings.TrimSpace(bundle.AccessToken) != "" { // swobu:io-string source=boundary
		return nil
	}
	nextBundle, err := requestChatGPTTokenRefresh(ctx, e.client, bundle.RefreshToken)
	if err != nil {
		return err
	}
	encoded, err := outboundcredentials.EncodeTokenBundle(nextBundle)
	if err != nil {
		return err
	}
	return outboundcredentials.StoreSecretByRef(providerSpec, credentialRef, encoded)
}

func requestChatGPTTokenRefresh(ctx context.Context, client *http.Client, refreshToken string) (outboundcredentials.TokenBundle, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", chatGPTOAuthClientID)
	form.Set("refresh_token", strings.TrimSpace(refreshToken)) // swobu:io-string source=boundary
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatGPTRefreshTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return outboundcredentials.TokenBundle{}, fmt.Errorf("token refresh request could not be built")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	resp, err := client.Do(req)
	if err != nil {
		return outboundcredentials.TokenBundle{}, fmt.Errorf("token refresh failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return outboundcredentials.TokenBundle{}, fmt.Errorf("token refresh returned status %d", resp.StatusCode)
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return outboundcredentials.TokenBundle{}, fmt.Errorf("token refresh response could not be decoded")
	}
	if strings.TrimSpace(payload.AccessToken) == "" { // swobu:io-string source=boundary
		return outboundcredentials.TokenBundle{}, fmt.Errorf("token refresh returned empty access token")
	}
	out := outboundcredentials.TokenBundle{
		AccessToken:  strings.TrimSpace(payload.AccessToken),  // swobu:io-string source=boundary
		RefreshToken: strings.TrimSpace(payload.RefreshToken), // swobu:io-string source=boundary
		IDToken:      strings.TrimSpace(payload.IDToken),      // swobu:io-string source=boundary
		IssuedAt:     time.Now().UTC(),
	}
	if out.RefreshToken == "" {
		out.RefreshToken = strings.TrimSpace(refreshToken) // swobu:io-string source=boundary
	}
	if payload.ExpiresIn > 0 {
		out.ExpiresAt = out.IssuedAt.Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	return out, nil
}

func (e ProviderExecutorAdapter) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	tier, ok := e.resolveChatGPTSubscriptionTier(ctx, target.ProviderID(), target.CredentialRef)
	if !ok {
		return nil, canonical.BadEndpoint("chatgpt subscription tier could not be resolved from credential")
	}
	models, ok := chatGPTTierModelIDs(tier)
	if !ok {
		return nil, canonical.BadEndpoint("chatgpt model catalog tier is unavailable in bundled list")
	}
	slog.Debug("chatgpt model catalog loaded from bundled lists",
		"backend_ref", strings.TrimSpace(target.BackendRef), // swobu:io-string source=boundary
		"tier", tier,
		"model_count", len(models),
	)
	return models, nil
}

func (e ProviderExecutorAdapter) resolveChatGPTSubscriptionTier(_ context.Context, providerSpec string, credentialRef string) (string, bool) {
	raw, err := outboundcredentials.ResolveStoredSecretByRef(providerSpec, credentialRef)
	if err != nil {
		return "", false
	}
	bundle, isBundle, err := outboundcredentials.DecodeTokenBundle(raw)
	if err != nil || !isBundle {
		return "", false
	}
	return parseChatGPTSubscriptionTierFromIDToken(bundle.IDToken)
}

func (e ProviderExecutorAdapter) ValidateCredentials(ctx context.Context, target ports.RoutableTarget) error {
	_, err := e.resolveAccessToken(ctx, target.ProviderID(), target.CredentialRef, false)
	return err
}

func parseChatGPTSubscriptionTierFromIDToken(idToken string) (string, bool) {
	idToken = strings.TrimSpace(idToken) // swobu:io-string source=boundary
	if idToken == "" {
		return "", false
	}
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	var claims struct {
		Auth struct {
			ChatGPTPlanType string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", false
	}
	planType := strings.ToLower(strings.TrimSpace(claims.Auth.ChatGPTPlanType)) // swobu:io-string source=provider-wire
	switch planType {
	case "free", "plus", "pro", "team":
		return planType, true
	default:
		return "", false
	}
}

func resolveChatGPTExecuteBaseURL(raw string) string {
	base := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if base == "" {
		return chatGPTCodexExecuteBase
	}
	lower := strings.ToLower(base) // swobu:io-string source=boundary
	if strings.Contains(lower, "chatgpt.com/backend-api/codex") {
		return strings.TrimRight(base, "/")
	}
	if strings.Contains(lower, "api.openai.com/v1") {
		return chatGPTCodexExecuteBase
	}
	return strings.TrimRight(base, "/")
}

func chatGPTResponseDecoder(providerIDRaw string, streaming bool, codec protocolregistry.EgressCodec) (providersruntime.ResponseDecoder, error) {
	if providerIDRaw != string(providercatalog.ProviderSpecChatGPT) {
		return nil, canonical.BadEndpoint("provider id is unsupported for chatgpt adapter runtime")
	}
	streamingDecoder := func(body io.ReadCloser) (ports.ProviderResponse, error) {
		return ports.NewEnvelopeStreamingProviderResponse(codec.DecodeResponseStream(body, "provider_stream:chatgpt_responses")), nil
	}
	bufferedDecoder := func(body io.ReadCloser) (ports.ProviderResponse, error) {
		defer func() { _ = body.Close() }()
		raw, err := io.ReadAll(body)
		if err != nil {
			return ports.ProviderResponse{}, canonical.InternalError("backend success response could not be read")
		}
		result, err := codec.DecodeResponse(raw)
		if err != nil {
			return ports.ProviderResponse{}, err
		}
		return ports.NewBufferedProviderResponse(result), nil
	}
	decoder, ok := providersruntime.SelectResponseDecoder(streaming, streamingDecoder, bufferedDecoder)
	if !ok {
		return nil, canonical.UnsupportedDelivery("chatgpt provider delivery variant is not implemented")
	}
	return decoder, nil
}

func resolveChatGPTProviderProtocol(providerProtocol string) (bool, error) {
	providerProtocol = strings.TrimSpace(providerProtocol) // swobu:io-string source=boundary
	if providerProtocol == "" || providerProtocol == providercatalog.ProviderProtocolAuto {
		return false, canonical.BadEndpoint("chatgpt provider protocol must be concrete")
	}
	if !providercatalog.SupportsProviderProtocolForSpec(string(providercatalog.ProviderSpecChatGPT), providerProtocol) {
		return false, canonical.BadEndpoint("selected provider protocol is unsupported for chatgpt")
	}
	switch providerProtocol {
	case "responses_stream":
		return true, nil
	default:
		return false, canonical.BadEndpoint("selected provider protocol is unsupported for chatgpt; use responses_stream")
	}
}
