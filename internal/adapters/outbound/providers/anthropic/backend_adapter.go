package anthropic

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

const (
	anthropicVersionHeaderValue = "2023-06-01"
	swobuCallerUAHeaderValue    = "swobu/dev"
)

type BackendAdapter struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
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

// NewRuntime builds a complete Anthropic provider runtime.
func NewRuntime(providerID profile.ProviderID, client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	executor := NewExecutor(client, credentials)
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         providerID,
		BackendResolver:    executor,
		CredentialProvider: credentials,
		Discovery:          executor,
	}
}

// ResolveBackend composes the exact Anthropic Messages backend.
func (e BackendAdapter) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	if err := validateAnthropicProviderProtocol(target.ProviderProtocol); err != nil {
		return provider.Backend{}, err
	}
	backend := provider.Backend{
		Target:    target.Clone(),
		Codec:     protocolcodec.Codec{ProviderID: target.ProviderID(), Protocol: protocolkind.Messages},
		Transport: provider.BindTransport(target, e.Send),
	}
	if err := backend.Validate(); err != nil {
		return provider.Backend{}, err
	}
	return backend, nil
}

// Send performs Anthropic HTTP transport over a final Messages document.
func (e BackendAdapter) Send(ctx context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
	if err := validateAnthropicProviderProtocol(target.ProviderProtocol); err != nil {
		return nil, err
	}
	if strings.TrimSpace(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("anthropic provider base URL is required")
	}
	if doc.IsEmpty() {
		return nil, canonical.InternalError("anthropic provider request document is required")
	}
	wireReqBody := doc.RawBytes()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, httpedge.JoinBaseURLAndPath(target.BaseURL, "/messages"), bytes.NewReader(wireReqBody))
	if err != nil {
		return nil, canonical.BadEndpoint("anthropic provider request could not be built")
	}
	if len(wireReqBody) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("anthropic-version", anthropicVersionHeaderValue)
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, target.ProviderID(), target.CredentialRef); err != nil {
		return nil, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, provider.TransportFailure(ctx, canonical.BadEndpoint("anthropic provider request failed before backend response"))
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
		return nil, httpedge.ReadBackendHTTPError(resp, target.TargetID)
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
		protocolkind.Messages,
		"application/json",
		resp.Header.Clone(),
		raw,
		carrier.Meta{},
	)}, nil
}

func isSSEContentType(raw string) bool {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(raw)) // swobu:io-string source=boundary
	return err == nil && strings.EqualFold(mediaType, "text/event-stream")
}

var _ provider.BackendResolver = BackendAdapter{}

func (e BackendAdapter) ListDeployments(ctx context.Context, target provider.TargetSnapshot) ([]profile.ProviderDeploymentRecord, error) {
	if strings.TrimSpace(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("anthropic provider base URL is required")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, httpedge.JoinBaseURLAndPath(target.BaseURL, "/models"), nil)
	if err != nil {
		return nil, canonical.BadEndpoint("anthropic provider model catalog request could not be built")
	}
	httpReq.Header.Set("anthropic-version", anthropicVersionHeaderValue)
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, target.ProviderID(), target.CredentialRef); err != nil {
		return nil, err
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("anthropic provider model catalog request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return nil, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, httpedge.ReadBackendHTTPError(resp, target.TargetID)
	}
	models, err := modelcatalogopenai.DecodeModelIDs(resp.Body)
	if err != nil {
		return nil, canonical.InternalError("backend model catalog could not be decoded")
	}
	supportedProtocols := profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecAnthropic))
	out := make([]profile.ProviderDeploymentRecord, 0, len(models))
	for _, modelID := range models {
		out = append(out, profile.NewProviderDeployment(
			modelID,
			modelID,
			string(profile.ProviderSpecAnthropic),
			"",
			string(profile.ProviderSpecAnthropic),
			supportedProtocols,
			"",
		))
	}
	return out, nil
}

func (e BackendAdapter) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	deployments, err := e.ListDeployments(ctx, target)
	return provider.TargetProbeResult{Deployments: deployments}, err
}

func validateAnthropicProviderProtocol(providerProtocol string) error {
	providerProtocol = strings.TrimSpace(providerProtocol) // swobu:io-string source=boundary
	if providerProtocol == "" || providerProtocol == profile.ProviderProtocolAuto {
		return canonical.BadEndpoint("anthropic provider protocol must be concrete")
	}
	if !profile.SupportsProviderProtocolForSpec(string(profile.ProviderSpecAnthropic), providerProtocol) {
		return canonical.BadEndpoint("selected provider protocol is unsupported for anthropic")
	}
	return nil
}

func (e BackendAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string) error {
	if strings.TrimSpace(credentialRef) == "" { // swobu:io-string source=boundary
		return canonical.BadEndpoint("anthropic provider credential reference is required")
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
	req.Header.Set("x-api-key", token)
	return nil
}
