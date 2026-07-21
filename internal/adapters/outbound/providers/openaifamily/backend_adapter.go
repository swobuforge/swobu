// selection, wire realization, and response decoding in one outbound seam.
package openaifamily

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

const maxBackendEvidence = 64 << 10

type BackendAdapter struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
	profile     ProviderRoutePolicy
}

const swobuCallerUAHeaderValue = "swobu/dev"

// NewExecutor builds the OpenAI-family provider wiring adapter around commodity HTTP transport.
func NewExecutor(client *http.Client, credentials providersruntime.CredentialProvider, profile ProviderRoutePolicy) BackendAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	if profile == nil {
		panic("openaifamily: route profile is required")
	}
	return BackendAdapter{
		client:      client,
		credentials: credentials,
		profile:     profile,
	}
}

// NewRuntime builds a complete OpenAI-family provider runtime for one provider policy.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider, profile ProviderRoutePolicy) providersruntime.ProviderRuntimeBundle {
	executor := NewExecutor(client, credentials, profile)
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         profile.ProviderID(),
		BackendResolver:    executor,
		CredentialProvider: credentials,
		Discovery:          executor,
	}
}

// ResolveBackend binds one exact OpenAI-family target to its codec and
// document-only transport without performing provider I/O.
func (e BackendAdapter) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	if parsed, ok := profile.ParseProviderID(strings.TrimSpace(target.ProviderID())); !ok || parsed != e.profile.ProviderID() { // swobu:io-string source=boundary
		return provider.Backend{}, canonical.BadEndpoint("provider policy is unsupported for exact backend")
	}
	backend := provider.Backend{
		Target:    target.Clone(),
		Codec:     protocolcodec.Codec{Protocol: target.ProtocolKind},
		Transport: provider.BindTransport(target, e.Send),
	}
	if err := backend.Validate(); err != nil {
		return provider.Backend{}, err
	}
	return backend, nil
}

// Send performs provider HTTP transport over an already-final wire document.
// It cannot observe canonical request state or exchange orchestration.
func (e BackendAdapter) Send(ctx context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
	if strings.TrimSpace(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("provider endpoint base URL is required")
	}
	if requiresExplicitCredentialRef(target.ProviderID(), target.BaseURL, target.CredentialRef) {
		return nil, canonical.BadEndpoint(providerCredentialRequiredMessage(target.ProviderID()))
	}

	if parsed, ok := profile.ParseProviderID(strings.TrimSpace(target.ProviderID())); !ok || parsed != e.profile.ProviderID() { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("provider policy is unsupported for HTTP adapter runtime")
	}
	wireReqCarrier := doc
	if wireReqCarrier.IsEmpty() {
		return nil, canonical.InternalError("provider request document is required")
	}
	path, err := profile.ProviderRequestPath(target.ProviderID(), target.ProtocolKind)
	if err != nil {
		return nil, err
	}
	wireReqBody := wireReqCarrier.RawBytes()
	requestStreaming := requestsStreamingResponse(wireReqBody)
	logOpenAIFamilyOutboundRequest(target.ProviderID(), target.ProviderProtocol, path, wireReqBody)
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		httpedge.JoinBaseURLAndPath(target.BaseURL, path),
		bytes.NewReader(wireReqBody),
	)
	if err != nil {
		badEndpoint := canonical.BadEndpoint("provider endpoint request could not be built")
		badEndpoint.Details = map[string]string{
			"request_build_error": detailErrorMessage(err), // swobu:io-string source=boundary
		}
		return nil, badEndpoint
	}
	if len(wireReqBody) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)

	if err := e.applyCredential(ctx, httpReq, target.ProviderID(), target.CredentialRef, target.AuthHeader); err != nil {
		return nil, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		// Preserve the causal transport error so request-outcome logs can tell
		// a connection/URL failure from the generic BAD_ENDPOINT wrapper.
		badEndpoint := canonical.BadEndpoint("provider endpoint request failed before backend response")
		badEndpoint.Details = map[string]string{
			"request_transport_error": detailErrorMessage(err), // swobu:io-string source=boundary
		}
		return nil, provider.TransportFailure(ctx, badEndpoint)
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
		backendErr := httpedge.ReadBackendHTTPError(resp, target.TargetID)
		classifiedErr := classifyBackendError(backendErr)
		if canonical.IsBackendErrorClass(classifiedErr, canonical.BackendErrorClassToolChoiceUnsupported) {
			return nil, provider.UnsupportedByBackend(classifiedErr)
		}
		return nil, classifiedErr
	}
	if requestStreaming && !isSSEContentType(resp.Header.Get("Content-Type")) {
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxBackendEvidence+1))
		if len(raw) > maxBackendEvidence {
			raw = raw[:maxBackendEvidence]
		}
		return nil, canonical.NewBackendError(
			target.TargetID,
			http.StatusBadGateway,
			strings.TrimSpace(string(raw)),                    // swobu:io-string source=boundary
			strings.TrimSpace(resp.Header.Get("Retry-After")), // swobu:io-string source=boundary
		)
	}
	if isSSEContentType(resp.Header.Get("Content-Type")) {
		return provider.StreamIngress{Stream: carrier.ByteStream{
			Header:    resp.Header.Clone(),
			MediaType: resp.Header.Get("Content-Type"),
			Body:      resp.Body,
		}}, nil
	}
	raw, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, canonical.InternalError("backend success response could not be read")
	}
	return provider.DocumentIngress{Document: carrier.NewDocument(
		target.ProtocolKind,
		"application/json",
		resp.Header.Clone(),
		raw,
		carrier.Meta{},
	)}, nil
}

func requestsStreamingResponse(raw []byte) bool {
	var payload struct {
		Stream bool `json:"stream"`
	}
	return json.Unmarshal(raw, &payload) == nil && payload.Stream
}

func isSSEContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(raw)) // swobu:io-string source=boundary
	return err == nil && strings.EqualFold(mediaType, "text/event-stream")
}

// applyCredential keeps auth resolution at the provider edge so canonicals and
// app orchestration never need to know provider token mechanics.
func (e BackendAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string, authHeader string) error {
	auth := AuthStrategyForHeader(authHeader, e.profile.AuthStrategy())
	if auth.Style == AuthStyleNone {
		return nil
	}
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
	auth.Apply(req, token)
	return nil
}

func requiresExplicitCredentialRef(providerSpec string, baseURL string, credentialRef string) bool {
	if strings.TrimSpace(credentialRef) != "" { // swobu:io-string source=boundary
		return false
	}
	return profile.RequiresCredential(strings.TrimSpace(providerSpec), strings.TrimSpace(baseURL)) // swobu:io-string source=boundary
}

func providerCredentialRequiredMessage(providerSpec string) string {
	providerID, ok := profile.ParseProviderID(strings.TrimSpace(providerSpec)) // swobu:io-string source=boundary
	if !ok {
		return "provider credential reference is required"
	}
	return string(providerID) + " provider credential reference is required"
}

func detailErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	if cause := errors.Unwrap(err); cause != nil {
		if message := strings.TrimSpace(cause.Error()); message != "" { // swobu:io-string source=boundary
			return message
		}
	}
	return strings.TrimSpace(err.Error()) // swobu:io-string source=boundary
}

func logOpenAIFamilyOutboundRequest(providerSpec string, providerProtocol string, path string, body []byte) {
	normalized, truncated := compactAndTruncateJSON(redactProviderRequestInput(body), 4096)
	slog.Debug("openaifamily outbound request",
		"component", "provider",
		"event", "outbound_request_shape",
		"provider_spec", strings.TrimSpace(providerSpec), // swobu:io-string source=boundary
		"provider_protocol", strings.TrimSpace(providerProtocol), // swobu:io-string source=boundary
		"path", strings.TrimSpace(path), // swobu:io-string source=boundary
		"body_bytes", len(body),
		"body_truncated", truncated,
		"body_json", normalized,
	)
}

func redactProviderRequestInput(body []byte) []byte {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return []byte(`{"request":"[REDACTED]"}`)
	}
	if _, present := payload["input"]; present {
		payload["input"] = json.RawMessage(`"[REDACTED]"`)
	}
	redacted, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{"request":"[REDACTED]"}`)
	}
	return redacted
}

func compactAndTruncateJSON(raw []byte, maxBytes int) (string, bool) {
	text := strings.TrimSpace(string(raw)) // swobu:io-string source=boundary
	if text == "" {
		return "null", false
	}
	normalized := text
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(text)); err == nil {
		normalized = compact.String()
	}
	if maxBytes <= 0 || len(normalized) <= maxBytes {
		return normalized, false
	}
	return normalized[:maxBytes], true
}
