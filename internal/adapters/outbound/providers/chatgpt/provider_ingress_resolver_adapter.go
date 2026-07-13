// swobu:lint ignore file-length because=provider edge behavior is intentionally localized in one executor owner seam
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
	providercompat "github.com/swobuforge/swobu/internal/adapters/outbound/providers/providercompat"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
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

// NewRuntime builds a complete ChatGPT provider runtime.
func NewRuntime(providerID profile.ProviderID, client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	executor := NewExecutor(client, credentials)
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         providerID,
		ProviderExecutor:   executor,
		CredentialProvider: credentials,
		Discovery:          executor,
	}
}

func (e ProviderIngressResolverAdapter) ResolveProviderIngress(ctx context.Context, req exchange.ProviderRequest) (exchange.ProviderIngress, error) {
	if strings.TrimSpace(req.Request.Model()) == "" { // swobu:io-string source=domain
		return nil, canonical.BadRequest("canonical request is required")
	}
	resolvedDelivery, err := resolveChatGPTDelivery(req.Target.ProviderProtocol)
	if err != nil {
		return nil, err
	}
	wireReqResult, err := codexwire.EncodeProviderRequestDocument(req.Request, resolvedDelivery, req.ExchangeID)
	if commitErr := providercompat.CommitEffects(ctx, req.EffectSink, req.ExchangeID, wireReqResult.Effects); commitErr != nil {
		return nil, commitErr
	}
	if err != nil {
		return nil, err
	}
	wireReq := wireReqResult.Value
	baseURL := resolveChatGPTExecuteBaseURL(req.Target.BaseURL)
	bodyBytes := wireReq.RawBytes()
	newRequest := func(token string) (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, httpedge.JoinBaseURLAndPath(baseURL, "/responses"), bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		if len(bodyBytes) > 0 {
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
		return nil, err
	}
	httpReq, err := newRequest(token)
	if err != nil {
		return nil, canonical.BadEndpoint("chatgpt provider request could not be built")
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("chatgpt provider request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return nil, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	if resp.StatusCode == http.StatusUnauthorized {
		backendErr := httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
		recoveredToken, refreshErr := e.resolveAccessToken(ctx, req.Target.ProviderID(), req.Target.CredentialRef, true)
		if refreshErr != nil || strings.TrimSpace(recoveredToken) == "" { // swobu:io-string source=boundary
			return nil, backendErr
		}
		retryReq, buildErr := newRequest(recoveredToken)
		if buildErr != nil {
			return nil, canonical.BadEndpoint("chatgpt provider request could not be built")
		}
		retryResp, retryErr := e.client.Do(retryReq)
		if retryErr != nil {
			return nil, canonical.BadEndpoint("chatgpt provider request failed before backend response")
		}
		retryResp, retryErr = httpedge.DecodeHTTPResponseContentEncoding(retryResp)
		if retryErr != nil {
			defer func() { _ = retryResp.Body.Close() }()
			return nil, canonical.InternalError("backend response content encoding is unsupported or invalid")
		}
		resp = retryResp
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		return nil, httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
	}
	if resolvedDelivery.IsStreaming() {
		return carrier.WireStream{
			Stage:   carrier.StageProviderIngressIn,
			Family:  req.Target.ProtocolKind,
			Framing: carrier.FramingSSE,
			Header:  resp.Header.Clone(),
			Frames:  carrier.FrameReaderFromReadCloser(resp.Body),
		}, nil
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, canonical.InternalError("backend success response could not be read")
	}
	return carrier.NewWireDocument(
		carrier.StageProviderIngressIn,
		req.Target.ProtocolKind,
		"application/json",
		resp.Header.Clone(),
		body,
		carrier.Meta{},
	), nil
}

func (e ProviderIngressResolverAdapter) resolveAccessToken(ctx context.Context, providerSpec string, credentialRef string, forceRefresh bool) (string, error) {
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

func (e ProviderIngressResolverAdapter) refreshCredentialBundle(ctx context.Context, providerSpec string, credentialRef string) error {
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

func (e ProviderIngressResolverAdapter) ListDeployments(ctx context.Context, target exchange.RoutableTarget) ([]profile.ProviderDeploymentRecord, error) {
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
	supportedProtocols := profile.ConcreteProviderProtocolsForSpec(target.ProviderID())
	out := make([]profile.ProviderDeploymentRecord, 0, len(models))
	for _, modelID := range models {
		out = append(out, profile.NewProviderDeployment(
			modelID,
			modelID,
			target.ProviderID(),
			"",
			target.ProviderID(),
			supportedProtocols,
			"",
		))
	}
	return out, nil
}

func (e ProviderIngressResolverAdapter) resolveChatGPTSubscriptionTier(_ context.Context, providerSpec string, credentialRef string) (string, bool) {
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

func (e ProviderIngressResolverAdapter) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
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

func resolveChatGPTDelivery(providerProtocol string) (delivery.Delivery, error) {
	providerProtocol = strings.TrimSpace(providerProtocol) // swobu:io-string source=boundary
	if providerProtocol == "" || providerProtocol == profile.ProviderProtocolAuto {
		return delivery.BufferedDelivery(), canonical.BadEndpoint("chatgpt provider protocol must be concrete")
	}
	if !profile.SupportsProviderProtocolForSpec(string(profile.ProviderSpecChatGPT), providerProtocol) {
		return delivery.BufferedDelivery(), canonical.BadEndpoint("selected provider protocol is unsupported for chatgpt")
	}
	switch providerProtocol {
	case "responses_stream":
		return delivery.StreamingDelivery(delivery.FramingSSE), nil
	default:
		return delivery.BufferedDelivery(), canonical.BadEndpoint("selected provider protocol is unsupported for chatgpt; use responses_stream")
	}
}
