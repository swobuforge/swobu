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
	"slices"
	"strings"
	"time"

	outboundcredentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

const (
	swobuCallerUAHeaderValue                = "swobu/dev"
	chatGPTCodexExecuteBase                 = "https://chatgpt.com/backend-api/codex"
	chatGPTCodexCatalogCompatibilityVersion = "0.153.3"
	chatGPTOriginatorHeaderKey              = "originator"
	chatGPTOriginatorHeaderValue            = "codex_cli_rs"
	chatGPTAccountIDHeaderKey               = "ChatGPT-Account-ID"
	chatGPTFedRAMPHeaderKey                 = "X-OpenAI-Fedramp"
	maxChatGPTCatalogBodyBytes              = 4 << 20
	maxChatGPTCatalogErrorBytes             = 64 << 10
)

var chatGPTRefreshTokenURL = "https://auth.openai.com/oauth/token"
var chatGPTOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

type BackendAdapter struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
}

type chatGPTAuthContext struct {
	AccessToken string
	AccountID   string
	IsFedRAMP   bool
}

type chatGPTModelCatalog struct {
	Models []chatGPTModel `json:"models"`
}

type chatGPTModel struct {
	Slug       string `json:"slug"`
	Visibility string `json:"visibility"`
	Priority   int    `json:"priority"`
}

func NewExecutor(client *http.Client, credentials providersruntime.CredentialProvider) BackendAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return BackendAdapter{
		client:      client,
		credentials: credentials,
	}
}

// NewRuntime builds a complete ChatGPT provider runtime.
func NewRuntime(providerID profile.ProviderID, client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	executor := NewExecutor(client, credentials)
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         providerID,
		BackendResolver:    executor,
		CredentialProvider: credentials,
		Discovery:          executor,
	}
}

// ResolveBackend composes one exact ChatGPT Codex backend.
func (e BackendAdapter) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	providerDelivery, err := resolveChatGPTDelivery(target.ProviderProtocol)
	if err != nil {
		return provider.Backend{}, err
	}
	if target.ProviderDelivery != providerDelivery {
		return provider.Backend{}, canonical.BadEndpoint("ChatGPT target delivery does not match its concrete provider protocol")
	}
	backend := provider.Backend{
		Target: target.Clone(), Codec: newBackendCodec(target.ProviderID()), Transport: provider.BindTransport(target, e.Send),
	}
	if err := backend.Validate(); err != nil {
		return provider.Backend{}, err
	}
	return backend, nil
}

// Send performs ChatGPT Codex HTTP transport over a final Responses document.
func (e BackendAdapter) Send(ctx context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
	providerDelivery, err := resolveChatGPTDelivery(target.ProviderProtocol)
	if err != nil {
		return nil, provider.AttemptNotDispatched(err)
	}
	if target.ProviderDelivery != providerDelivery {
		return nil, provider.AttemptNotDispatched(canonical.BadEndpoint("ChatGPT target delivery does not match its concrete provider protocol"))
	}
	if doc.IsEmpty() {
		return nil, provider.AttemptNotDispatched(canonical.InternalError("chatgpt provider request document is required"))
	}
	baseURL := resolveChatGPTExecuteBaseURL(target.BaseURL)
	bodyBytes := doc.RawBytes()
	newRequest := func(auth chatGPTAuthContext) (*http.Request, error) {
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
		applyChatGPTAuthHeaders(httpReq, auth)
		return httpReq, nil
	}
	auth, err := e.resolveAuthContext(ctx, target.ProviderID(), target.CredentialRef, false)
	if err != nil {
		return nil, provider.AttemptNotDispatched(err)
	}
	httpReq, err := newRequest(auth)
	if err != nil {
		return nil, provider.AttemptNotDispatched(canonical.BadEndpoint("chatgpt provider request could not be built"))
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, provider.TransportFailure(ctx, err)
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return nil, provider.AttemptMayHaveExecuted(canonical.InternalError("backend response content encoding is unsupported or invalid"))
	}
	if resp.StatusCode == http.StatusUnauthorized {
		// A 401 is a positive pre-execution authentication rejection. Refreshing
		// the credential and rebuilding this exact request is the sole
		// transport-owned POST replay; it cannot duplicate provider work.
		backendErr := httpedge.ReadBackendHTTPError(resp, target.TargetID)
		_ = resp.Body.Close()
		recoveredAuth, refreshErr := e.resolveAuthContext(ctx, target.ProviderID(), target.CredentialRef, true)
		if refreshErr != nil || strings.TrimSpace(recoveredAuth.AccessToken) == "" { // swobu:io-string source=boundary
			return nil, provider.AttemptRejectedBeforeExecution(backendErr)
		}
		retryReq, buildErr := newRequest(recoveredAuth)
		if buildErr != nil {
			return nil, provider.AttemptRejectedBeforeExecution(canonical.BadEndpoint("chatgpt provider request could not be rebuilt after authentication rejection"))
		}
		retryResp, retryErr := e.client.Do(retryReq)
		if retryErr != nil {
			return nil, provider.TransportFailure(ctx, retryErr)
		}
		retryResp, retryErr = httpedge.DecodeHTTPResponseContentEncoding(retryResp)
		if retryErr != nil {
			defer func() { _ = retryResp.Body.Close() }()
			return nil, provider.AttemptMayHaveExecuted(canonical.InternalError("backend response content encoding is unsupported or invalid"))
		}
		resp = retryResp
	}
	rawContentType := strings.TrimSpace(resp.Header.Get("Content-Type")) // swobu:io-string source=boundary
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		backendErr := httpedge.ReadBackendHTTPError(resp, target.TargetID)
		return nil, provider.AttemptMayHaveExecuted(protocolcodec.ParseBackendError(backendErr, protocolkind.Responses, resp.Header.Get("x-request-id")))
	}
	if rawContentType != "" && !httpedge.IsEventStreamContentType(rawContentType) {
		return nil, provider.AttemptMayHaveExecuted(httpedge.ReadUnexpectedStreamingResponse(resp, target.TargetID))
	}
	// ChatGPT's SSE-only Codex endpoint can omit Content-Type on a successful
	// stream. Normalize only absence at this exact-provider edge; an explicit
	// non-SSE value remains a backend representation failure.
	streamMediaType := rawContentType
	if streamMediaType == "" {
		streamMediaType = "text/event-stream"
	}
	return provider.StreamIngress{Stream: carrier.ByteStream{
		Header:    resp.Header.Clone(),
		MediaType: streamMediaType,
		Body:      resp.Body,
	}}, nil
}

var _ provider.BackendResolver = BackendAdapter{}

func (e BackendAdapter) resolveAuthContext(ctx context.Context, providerSpec string, credentialRef string, forceRefresh bool) (chatGPTAuthContext, error) {
	if strings.TrimSpace(credentialRef) == "" { // swobu:io-string source=boundary
		return chatGPTAuthContext{}, canonical.BadEndpoint("chatgpt provider credential reference is required")
	}
	if e.credentials == nil {
		return chatGPTAuthContext{}, canonical.BadEndpoint("credential resolver is not configured")
	}
	if forceRefresh {
		return e.refreshCredentialBundle(ctx, providerSpec, credentialRef)
	}
	return e.resolveCredentialSnapshot(ctx, providerSpec, credentialRef)
}

func (e BackendAdapter) resolveCredentialSnapshot(ctx context.Context, providerSpec string, credentialRef string) (chatGPTAuthContext, error) {
	refKind := credentialref.Parse(credentialRef).Kind()
	_, usesCanonicalResolver := e.credentials.(outboundcredentials.CredentialSourceResolverRegistry)
	if usesCanonicalResolver && (refKind == credentialref.KindSecret || refKind == credentialref.KindSecretFile) {
		// The canonical resolver and raw snapshot reader share one stored-secret
		// authority. Injected resolvers remain sole owners of their opaque refs.
		raw, err := outboundcredentials.ResolveStoredSecretByRef(providerSpec, credentialRef)
		if err != nil {
			return chatGPTAuthContext{}, canonical.BadEndpoint("credential reference could not be resolved")
		}
		return chatGPTAuthContextFromStoredSnapshot(raw)
	}
	token, err := e.credentials.ResolveCredential(ctx, providerSpec, credentialRef)
	if err != nil {
		return chatGPTAuthContext{}, canonical.BadEndpoint("credential reference could not be resolved")
	}
	token = strings.TrimSpace(token) // swobu:io-string source=boundary
	if token == "" {
		return chatGPTAuthContext{}, canonical.BadEndpoint("credential reference resolved to an empty token")
	}
	return chatGPTAuthContext{AccessToken: token}, nil
}

func chatGPTAuthContextFromStoredSnapshot(raw string) (chatGPTAuthContext, error) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=boundary
	bundle, isBundle, err := outboundcredentials.DecodeTokenBundle(trimmed)
	if err != nil {
		// Stored raw bearer tokens predate refresh-capable bundles. They remain
		// valid auth snapshots but intentionally carry no account routing.
		if trimmed != "" && !strings.HasPrefix(trimmed, "{") {
			return chatGPTAuthContext{AccessToken: trimmed}, nil
		}
		return chatGPTAuthContext{}, canonical.BadEndpoint("credential reference could not be resolved")
	}
	if !isBundle {
		return chatGPTAuthContext{}, canonical.BadEndpoint("credential reference could not be resolved")
	}
	accountID, isFedRAMP := parseChatGPTRoutingClaims(bundle.IDToken)
	return chatGPTAuthContext{AccessToken: bundle.AccessToken, AccountID: accountID, IsFedRAMP: isFedRAMP}, nil
}

func (e BackendAdapter) refreshCredentialBundle(ctx context.Context, providerSpec string, credentialRef string) (chatGPTAuthContext, error) {
	raw, err := outboundcredentials.ResolveStoredSecretByRef(providerSpec, credentialRef)
	if err != nil {
		return chatGPTAuthContext{}, err
	}
	bundle, isBundle, err := outboundcredentials.DecodeTokenBundle(raw)
	if err != nil || !isBundle {
		return chatGPTAuthContext{}, fmt.Errorf("credential is not refreshable")
	}
	if strings.TrimSpace(bundle.RefreshToken) == "" { // swobu:io-string source=boundary
		return chatGPTAuthContext{}, fmt.Errorf("credential is not refreshable")
	}
	nextBundle, err := requestChatGPTTokenRefresh(ctx, e.client, bundle.RefreshToken)
	if err != nil {
		return chatGPTAuthContext{}, err
	}
	if nextBundle.RefreshToken == "" {
		nextBundle.RefreshToken = bundle.RefreshToken
	}
	priorAccountID, _ := parseChatGPTRoutingClaims(bundle.IDToken)
	nextAccountID, _ := parseChatGPTRoutingClaims(nextBundle.IDToken)
	if nextBundle.IDToken == "" || (priorAccountID != "" && nextAccountID == "") {
		// Account routing outlives individual access and ID tokens. Retaining the
		// last routing-bearing ID token prevents a valid refresh response from
		// silently degrading a workspace credential into an unscoped request.
		nextBundle.IDToken = bundle.IDToken
	}
	encoded, err := outboundcredentials.EncodeTokenBundle(nextBundle)
	if err != nil {
		return chatGPTAuthContext{}, err
	}
	if err := outboundcredentials.StoreSecretByRef(providerSpec, credentialRef, encoded); err != nil {
		return chatGPTAuthContext{}, err
	}
	return chatGPTAuthContextFromStoredSnapshot(encoded)
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
	if payload.ExpiresIn > 0 {
		out.ExpiresAt = out.IssuedAt.Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	return out, nil
}

func (e BackendAdapter) ListDeployments(ctx context.Context, target provider.TargetSnapshot) ([]profile.ModelAuthoringOption, error) {
	auth, err := e.resolveAuthContext(ctx, target.ProviderID(), target.CredentialRef, false)
	if err != nil {
		return nil, err
	}
	models, status, err := e.fetchModelCatalog(ctx, target, auth)
	if status == http.StatusUnauthorized {
		auth, err = e.resolveAuthContext(ctx, target.ProviderID(), target.CredentialRef, true)
		if err != nil {
			logChatGPTCatalogOutcome(target, auth, status, "unauthorized", 0)
			return nil, canonical.NewBackendError(target.TargetID, status, "chatgpt model catalog authentication failed", "")
		}
		models, status, err = e.fetchModelCatalog(ctx, target, auth)
	}
	if err != nil {
		logChatGPTCatalogOutcome(target, auth, status, "failed", 0)
		return nil, err
	}
	logChatGPTCatalogOutcome(target, auth, status, "ok", len(models))
	supportedProtocols := profile.ConcreteProviderProtocolsForSpec(target.ProviderID())
	out := make([]profile.ModelAuthoringOption, 0, len(models))
	for _, modelID := range models {
		out = append(out, profile.NewModelAuthoringOption(
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

func logChatGPTCatalogOutcome(target provider.TargetSnapshot, auth chatGPTAuthContext, status int, outcome string, visibleModelCount int) {
	slog.Debug("chatgpt model catalog outcome",
		"provider", target.ProviderID(),
		"operation", "model_catalog",
		"status", outcome,
		"http_status", status,
		"visible_model_count", visibleModelCount,
		"account_routing", auth.AccountID != "",
		"fedramp", auth.IsFedRAMP,
		"client_version", chatGPTCodexCatalogCompatibilityVersion,
	)
}

func (e BackendAdapter) fetchModelCatalog(ctx context.Context, target provider.TargetSnapshot, auth chatGPTAuthContext) ([]string, int, error) {
	baseURL := resolveChatGPTExecuteBaseURL(target.BaseURL)
	catalogURL := httpedge.JoinBaseURLAndPath(baseURL, "/models")
	parsedURL, err := url.Parse(catalogURL)
	if err != nil {
		return nil, 0, canonical.BadEndpoint("chatgpt model catalog URL is invalid")
	}
	query := parsedURL.Query()
	query.Set("client_version", chatGPTCodexCatalogCompatibilityVersion)
	parsedURL.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return nil, 0, canonical.BadEndpoint("chatgpt model catalog request could not be built")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	applyChatGPTAuthHeaders(req, auth)
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, 0, canonical.BadEndpoint("chatgpt model catalog request failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxChatGPTCatalogErrorBytes))
		message := strings.TrimSpace(string(raw)) // swobu:io-string source=provider-wire
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return nil, resp.StatusCode, canonical.NewBackendError(target.TargetID, resp.StatusCode, message, strings.TrimSpace(resp.Header.Get("Retry-After")))
	}
	var catalog chatGPTModelCatalog
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxChatGPTCatalogBodyBytes+1))
	if err != nil || len(raw) > maxChatGPTCatalogBodyBytes || json.Unmarshal(raw, &catalog) != nil {
		return nil, resp.StatusCode, canonical.BadEndpoint("chatgpt model catalog response could not be decoded")
	}
	return projectChatGPTModels(catalog.Models), resp.StatusCode, nil
}

func projectChatGPTModels(models []chatGPTModel) []string {
	visibleBySlug := make(map[string]chatGPTModel, len(models))
	for _, model := range models {
		model.Slug = strings.TrimSpace(model.Slug)             // swobu:io-string source=provider-wire
		model.Visibility = strings.TrimSpace(model.Visibility) // swobu:io-string source=provider-wire
		if model.Slug == "" || model.Visibility != "list" {
			continue
		}
		if prior, ok := visibleBySlug[model.Slug]; !ok || model.Priority < prior.Priority {
			visibleBySlug[model.Slug] = model
		}
	}
	visible := make([]chatGPTModel, 0, len(visibleBySlug))
	for _, model := range visibleBySlug {
		visible = append(visible, model)
	}
	slices.SortFunc(visible, func(left chatGPTModel, right chatGPTModel) int {
		if left.Priority != right.Priority {
			return left.Priority - right.Priority
		}
		return strings.Compare(left.Slug, right.Slug)
	})
	out := make([]string, 0, len(visible))
	for _, model := range visible {
		out = append(out, model.Slug)
	}
	return out
}

func (e BackendAdapter) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	deployments, err := e.ListDeployments(ctx, target)
	return provider.TargetProbeResult{Options: deployments}, err
}

func parseChatGPTRoutingClaims(idToken string) (string, bool) {
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
			AccountID string `json:"chatgpt_account_id"`
			IsFedRAMP bool   `json:"chatgpt_account_is_fedramp"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", false
	}
	return strings.TrimSpace(claims.Auth.AccountID), claims.Auth.IsFedRAMP // swobu:io-string source=provider-wire
}

func applyChatGPTAuthHeaders(req *http.Request, auth chatGPTAuthContext) {
	req.Header.Set("Authorization", "Bearer "+auth.AccessToken)
	req.Header.Set(chatGPTOriginatorHeaderKey, chatGPTOriginatorHeaderValue)
	if auth.AccountID != "" {
		req.Header.Set(chatGPTAccountIDHeaderKey, auth.AccountID)
	}
	if auth.IsFedRAMP {
		req.Header.Set(chatGPTFedRAMPHeaderKey, "true")
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
	if providerProtocol == "" {
		return delivery.BufferedDelivery(), canonical.BadEndpoint("chatgpt provider protocol must be concrete")
	}
	if !profile.SupportsProviderProtocolForSpec(string(profile.ProviderSpecChatGPT), providerProtocol) {
		return delivery.BufferedDelivery(), canonical.BadEndpoint("selected provider protocol is unsupported for chatgpt")
	}
	spec, ok := profile.ProviderProtocolSpecForSpec(string(profile.ProviderSpecChatGPT), providerProtocol)
	if !ok || spec.Kind != protocolkind.Responses {
		return delivery.BufferedDelivery(), canonical.BadEndpoint("selected provider protocol is unsupported for chatgpt; use responses_stream")
	}
	return spec.Delivery, nil
}
