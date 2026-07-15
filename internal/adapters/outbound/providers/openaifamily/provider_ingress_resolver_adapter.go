// selection, wire realization, and response decoding in one outbound seam.
package openaifamily

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	providercompat "github.com/swobuforge/swobu/internal/adapters/outbound/providers/providercompat"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/effect"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
)

type ProviderIngressResolverAdapter struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
	profile     ProviderRoutePolicy
}

const swobuCallerUAHeaderValue = "swobu/dev"

// NewExecutor builds the OpenAI-family provider wiring adapter around commodity HTTP transport.
func NewExecutor(client *http.Client, credentials providersruntime.CredentialProvider, profile ProviderRoutePolicy) ProviderIngressResolverAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	if profile == nil {
		panic("openaifamily: route profile is required")
	}
	return ProviderIngressResolverAdapter{
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
		ProviderExecutor:   executor,
		CredentialProvider: credentials,
		Discovery:          executor,
	}
}

// Execute performs provider HTTP transport only. Exchange orchestration and
// semantic decode live in exchange while provider-edge wire patchers live in
// provider-owned helper packages.
func (e ProviderIngressResolverAdapter) ResolveProviderIngress(ctx context.Context, req exchange.ProviderRequest) (exchange.ProviderIngress, error) {
	if strings.TrimSpace(req.Request.Model()) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadRequest("canonical request is required")
	}
	if strings.TrimSpace(req.Target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("OpenAI-family provider base URL is required")
	}
	if requiresExplicitCredentialRef(req.Target.ProviderID(), req.Target.BaseURL, req.Target.CredentialRef) {
		return nil, canonical.BadEndpoint(providerCredentialRequiredMessage(req.Target.ProviderID()))
	}

	if parsed, ok := profile.ParseProviderID(strings.TrimSpace(req.Target.ProviderID())); !ok || parsed != e.profile.ProviderID() { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("provider policy is unsupported for OpenAI-family adapter runtime")
	}
	if err := req.Contract.ProviderDelivery.Validate(); err != nil {
		return nil, canonical.UnsupportedDelivery("OpenAI-family provider delivery is unsupported")
	}
	if req.Contract.ProviderDelivery.IsStreaming() && req.Contract.ProviderDelivery.Framing != delivery.FramingSSE {
		return nil, canonical.UnsupportedDelivery("OpenAI-family provider does not implement the requested delivery framing")
	}
	wireReqCarrier := req.RequestDocument
	if wireReqCarrier.IsEmpty() {
		return nil, canonical.InternalError("provider request document is required")
	}
	if err := providercompat.EmitStructuredOutputDecisions(ctx, req.EffectSink, req.ExchangeID, req.Target.ProviderID(), req.Target.ProtocolKind, req.Request.OutputFormat()); err != nil {
		return nil, err
	}
	if err := providercompat.EmitToolSchemaStrictDecision(ctx, req.EffectSink, req.ExchangeID, req.Target.ProviderID(), req.Target.ProtocolKind, req.Request.Tools(), true); err != nil {
		return nil, err
	}
	path, err := profile.ProviderRequestPath(req.Target.ProviderID(), req.Target.ProtocolKind)
	if err != nil {
		return nil, err
	}
	wireReqBody := wireReqCarrier.RawBytes()
	logOpenAIFamilyOutboundRequest(req.ExchangeID, req.Target.ProviderID(), req.Target.ProviderProtocol, path, wireReqBody)
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		httpedge.JoinBaseURLAndPath(req.Target.BaseURL, path),
		bytes.NewReader(wireReqBody),
	)
	if err != nil {
		badEndpoint := canonical.BadEndpoint("OpenAI-family provider request could not be built")
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

	if err := e.applyCredential(ctx, httpReq, req.Target.ProviderID(), req.Target.CredentialRef, req.Target.AuthHeader); err != nil {
		return nil, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		// Preserve the causal transport error so request-outcome logs can tell
		// a connection/URL failure from the generic BAD_ENDPOINT wrapper.
		badEndpoint := canonical.BadEndpoint("OpenAI-family provider request failed before backend response")
		badEndpoint.Details = map[string]string{
			"request_transport_error": detailErrorMessage(err), // swobu:io-string source=boundary
		}
		return nil, badEndpoint
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
		backendErr := httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
		classifiedErr := classifyBackendError(backendErr)
		emitBackendErrorClassDecision(ctx, req.EffectSink, req.ExchangeID, req.Target.ProviderID(), req.Target.ProtocolKind, classifiedErr)
		return nil, classifiedErr
	}
	if req.Contract.ProviderDelivery.IsStreaming() {
		return carrier.CarrierStream{
			Stage:   carrier.StageProviderIngressIn,
			Family:  req.Target.ProtocolKind,
			Framing: carrier.FramingSSE,
			Header:  resp.Header.Clone(),
			Frames:  carrier.FrameReaderFromReadCloser(resp.Body),
		}, nil
	}
	raw, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, canonical.InternalError("backend success response could not be read")
	}
	return carrier.NewCarrierDocument(
		carrier.StageProviderIngressIn,
		req.Target.ProtocolKind,
		"application/json",
		resp.Header.Clone(),
		raw,
		carrier.Meta{},
	), nil
}

// applyCredential keeps auth resolution at the provider edge so canonicals and
// app orchestration never need to know provider token mechanics.
func (e ProviderIngressResolverAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string, authHeader string) error {
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

func logOpenAIFamilyOutboundRequest(exchangeID string, providerSpec string, providerProtocol string, path string, body []byte) {
	normalized, truncated := compactAndTruncateJSON(body, 4096)
	slog.Debug("openaifamily outbound request",
		"component", "provider",
		"event", "outbound_request_shape",
		"exchange_id", strings.TrimSpace(exchangeID), // swobu:io-string source=boundary
		"provider_spec", strings.TrimSpace(providerSpec), // swobu:io-string source=boundary
		"provider_protocol", strings.TrimSpace(providerProtocol), // swobu:io-string source=boundary
		"path", strings.TrimSpace(path), // swobu:io-string source=boundary
		"body_bytes", len(body),
		"body_truncated", truncated,
		"body_json", normalized,
	)
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

func emitBackendErrorClassDecision(ctx context.Context, sink effect.Sink, exchangeID string, providerID string, protocol protocolkind.ProtocolKind, classifiedErr error) {
	if sink == nil {
		return
	}
	var backendClassifiedErr canonical.ClassifiedBackendError
	if !errors.As(classifiedErr, &backendClassifiedErr) {
		return
	}
	subject := backendErrorClassSubject(routeDecisionSubject(providerID, string(protocol)), backendClassifiedErr.Class)
	if subject == "" {
		return
	}
	_ = sink.Commit(ctx, exchangeID, []effect.Effect{
		effect.CompatibilityEffect{
			Feature: compat.ErrorClass,
			Outcome: compat.Approx,
			Subject: subject,
		},
	})
}

func routeDecisionSubject(providerID string, protocol string) compat.Subject {
	protocol = strings.TrimSpace(protocol) // swobu:io-string source=boundary
	if providerID == "" || protocol == "" {
		return ""
	}
	return compat.Subject("route:provider/" + providerID + "/protocol/" + protocol)
}

func backendErrorClassSubject(routeSubject compat.Subject, class canonical.BackendErrorClass) compat.Subject {
	routeSubject = compat.Subject(strings.TrimSpace(string(routeSubject))) // swobu:io-string source=boundary
	class = canonical.BackendErrorClass(strings.TrimSpace(string(class)))  // swobu:io-string source=boundary
	if routeSubject == "" || class == "" {
		return ""
	}
	return compat.Subject(string(routeSubject) + "/error_class/" + string(class))
}
