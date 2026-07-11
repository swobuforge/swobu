// selection, wire realization, and response decoding in one outbound seam.
package openaifamily

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/transform"
)

type ProviderExecutorAdapter struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
	profile     ProviderRoutePolicy
}

const swobuCallerUAHeaderValue = "swobu/dev"

// NewExecutor builds the OpenAI-family provider wiring adapter around commodity HTTP transport.
func NewExecutor(client *http.Client, credentials providersruntime.CredentialProvider, profile ProviderRoutePolicy) ProviderExecutorAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	if profile == nil {
		panic("openaifamily: route profile is required")
	}
	return ProviderExecutorAdapter{
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
		Executor:           executor,
		CredentialProvider: credentials,
		ModelCatalogClient: executor,
	}
}

// Execute applies provider wiring, performs the backend HTTP call, and decodes
// successful responses into canonical semantics. Backend-origin failures remain
// backend errors rather than being normalized into Swobu success envelopes.
func (e ProviderExecutorAdapter) Execute(ctx context.Context, req ports.ProviderRequest) (ports.ProviderTransportResponse, error) {
	if strings.TrimSpace(req.Request.Model()) == "" { // swobu:io-string source=boundary
		return ports.ProviderTransportResponse{}, canonical.BadRequest("canonical request is required")
	}
	if strings.TrimSpace(req.Target.BaseURL) == "" { // swobu:io-string source=boundary
		return ports.ProviderTransportResponse{}, canonical.BadEndpoint("OpenAI-family provider base URL is required")
	}
	if requiresExplicitCredentialRef(req.Target.ProviderID(), req.Target.BaseURL, req.Target.CredentialRef) {
		return ports.ProviderTransportResponse{}, canonical.BadEndpoint(providerCredentialRequiredMessage(req.Target.ProviderID()))
	}

	dispatch, err := exchange.ResolveProviderProtocolRouting(req.Target, "OpenAI-family provider protocol must be concrete")
	if err != nil {
		return ports.ProviderTransportResponse{}, err
	}

	if parsed, ok := profile.ParseProviderID(strings.TrimSpace(req.Target.ProviderID())); !ok || parsed != e.profile.ProviderID() { // swobu:io-string source=boundary
		return ports.ProviderTransportResponse{}, canonical.BadEndpoint("provider policy is unsupported for OpenAI-family adapter runtime")
	}
	facts := e.profile.Facts(req.Request)
	wireReqCarrier, err := exchange.RealizeProviderRequestCarrier(
		req.Request,
		dispatch.Kind,
		dispatch.Delivery,
		"OpenAI-family provider does not implement the messages protocol",
	)
	if err != nil {
		return ports.ProviderTransportResponse{}, err
	}
	wireReqCarrier, transformReports, transformStageReports, transformNotices, err := transform.ApplyProviderWireOutStage(wireReqCarrier, newTransformRegistry(facts))
	if err != nil {
		return ports.ProviderTransportResponse{}, err
	}
	transformNotices = append(transformNotices, transformFactNotices(facts)...)
	path, err := exchange.ProviderRequestPathForProtocol(dispatch.Kind)
	if err != nil {
		return ports.ProviderTransportResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		httpedge.JoinBaseURLAndPath(req.Target.BaseURL, path),
		bytes.NewReader(wireReqCarrier.Raw),
	)
	if err != nil {
		return ports.ProviderTransportResponse{}, canonical.BadEndpoint("OpenAI-family provider request could not be built")
	}
	if len(wireReqCarrier.Raw) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)

	if err := e.applyCredential(ctx, httpReq, req.Target.ProviderID(), req.Target.CredentialRef); err != nil {
		return ports.ProviderTransportResponse{}, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return ports.ProviderTransportResponse{}, canonical.BadEndpoint("OpenAI-family provider request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
		return ports.ProviderTransportResponse{}, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	if resp.StatusCode >= 400 {
		defer func() {
			_ = resp.Body.Close()
		}()
		backendErr := httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
		return ports.ProviderTransportResponse{}, classifyBackendError(backendErr)
	}
	_ = transformReports
	_ = transformStageReports
	_ = transformNotices
	if dispatch.Delivery.Mode == delivery.Streaming {
		return ports.ProviderTransportResponse{
			Header: resp.Header.Clone(),
			Stream: resp.Body,
		}, nil
	}
	raw, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return ports.ProviderTransportResponse{}, canonical.InternalError("backend success response could not be read")
	}
	return ports.ProviderTransportResponse{
		Header:   resp.Header.Clone(),
		Document: raw,
	}, nil
}

// applyCredential keeps auth resolution at the provider edge so canonicals and
// app orchestration never need to know provider token mechanics.
func (e ProviderExecutorAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string) error {
	auth := e.profile.AuthStrategy()
	if auth.Style == authStyleNone {
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
	auth.apply(req, token)
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
