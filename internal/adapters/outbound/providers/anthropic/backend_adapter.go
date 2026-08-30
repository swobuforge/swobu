package anthropic

import (
	"bytes"
	"context"
	"io"
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
	providerID  profile.ProviderID
	client      *http.Client
	credentials providersruntime.CredentialProvider
}

func NewExecutor(client *http.Client, credentials providersruntime.CredentialProvider) BackendAdapter {
	return NewBackendAdapter(profile.ProviderSpecAnthropic, client, credentials)
}

// NewBackendAdapter builds the shared Messages transport for one provider
// whose profile declares Anthropic Messages protocols and x-api-key auth.
func NewBackendAdapter(providerID profile.ProviderID, client *http.Client, credentials providersruntime.CredentialProvider) BackendAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return BackendAdapter{
		providerID:  providerID,
		client:      client,
		credentials: credentials,
	}
}

// NewRuntime builds a complete Anthropic provider runtime.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	executor := NewBackendAdapter(profile.ProviderSpecAnthropic, client, credentials)
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         profile.ProviderSpecAnthropic,
		BackendResolver:    executor,
		CredentialProvider: credentials,
		Discovery:          executor,
	}
}

// ResolveBackend composes the exact Anthropic Messages backend.
func (e BackendAdapter) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	if err := e.validateProviderTarget(target); err != nil {
		return provider.Backend{}, err
	}
	backend := provider.Backend{
		Target: target.Clone(),
		Codec: protocolcodec.Codec{
			Protocol: protocolkind.Messages,
			MessagesDialect: protocolcodec.MessagesDialect{
				Lowering: protocolcodec.MessagesLowering{Tools: protocolcodec.MessagesToolLowering{WebSearch: protocolcodec.MessagesHostedSearchTool("web_search_20260209", "direct")}},
			},
		},
		Transport: provider.BindTransport(target, e.Send),
	}
	if err := backend.Validate(); err != nil {
		return provider.Backend{}, err
	}
	return backend, nil
}

// Send performs Anthropic HTTP transport over a final Messages document.
func (e BackendAdapter) Send(ctx context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
	if err := e.validateProviderTarget(target); err != nil {
		return nil, provider.AttemptNotDispatched(err)
	}
	if strings.TrimSpace(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, provider.AttemptNotDispatched(canonical.BadEndpoint("messages provider base URL is required"))
	}
	if doc.IsEmpty() {
		return nil, provider.AttemptNotDispatched(canonical.InternalError("messages provider request document is required"))
	}
	wireReqBody := doc.RawBytes()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, httpedge.JoinBaseURLAndPath(target.BaseURL, "/messages"), bytes.NewReader(wireReqBody))
	if err != nil {
		return nil, provider.AttemptNotDispatched(canonical.BadEndpoint("messages provider request could not be built"))
	}
	if len(wireReqBody) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("anthropic-version", anthropicVersionHeaderValue)
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := e.applyCredential(ctx, httpReq, target.ProviderID(), target.CredentialRef); err != nil {
		return nil, provider.AttemptNotDispatched(err)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, provider.TransportFailure(ctx, err)
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
		return nil, provider.AttemptMayHaveExecuted(canonical.InternalError("backend response content encoding is unsupported or invalid"))
	}
	if resp.StatusCode >= 400 {
		defer func() {
			_ = resp.Body.Close()
		}()
		return nil, provider.AttemptMayHaveExecuted(httpedge.ReadBackendHTTPError(resp, target.TargetID))
	}
	if httpedge.IsEventStreamContentType(resp.Header.Get("Content-Type")) {
		return provider.StreamIngress{Stream: carrier.ByteStream{
			Header:    resp.Header.Clone(),
			MediaType: resp.Header.Get("Content-Type"),
			Body:      resp.Body,
		}}, nil
	}
	raw, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return nil, provider.AttemptMayHaveExecuted(canonical.InternalError("backend success response could not be read"))
	}
	return provider.DocumentIngress{Document: carrier.NewDocument(
		protocolkind.Messages,
		"application/json",
		resp.Header.Clone(),
		raw,
		carrier.Meta{},
	)}, nil
}

var _ provider.BackendResolver = BackendAdapter{}

func (e BackendAdapter) ListDeployments(ctx context.Context, target provider.TargetSnapshot) ([]profile.ModelAuthoringOption, error) {
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
		return nil, provider.TransportFailure(ctx, err)
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
	supportedProtocols := profile.ConcreteProviderProtocolsForSpec(string(e.providerID))
	out := make([]profile.ModelAuthoringOption, 0, len(models))
	for _, modelID := range models {
		out = append(out, profile.NewModelAuthoringOption(
			modelID,
			modelID,
			string(e.providerID),
			"",
			string(e.providerID),
			supportedProtocols,
			"",
		))
	}
	return out, nil
}

func (e BackendAdapter) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	deployments, err := e.ListDeployments(ctx, target)
	return provider.TargetProbeResult{Options: deployments}, err
}

func (e BackendAdapter) validateProviderTarget(target provider.TargetSnapshot) error {
	if target.ProviderID() != string(e.providerID) {
		return canonical.BadEndpoint("selected provider does not match messages adapter")
	}
	return validateProviderProtocol(e.providerID, target)
}

// validateProviderProtocol proves the catalog token, canonical protocol kind,
// and transport frame all identify one Messages execution mode. Catalog
// admission alone is insufficient for providers such as Azure that expose
// multiple protocol families through separate shared adapters.
func validateProviderProtocol(providerID profile.ProviderID, target provider.TargetSnapshot) error {
	providerProtocol := strings.TrimSpace(target.ProviderProtocol) // swobu:io-string source=boundary
	if providerProtocol == "" {
		return canonical.BadEndpoint("messages provider protocol must be concrete")
	}
	kind, ok := profile.ProviderProtocolKind(string(providerID), providerProtocol)
	if !ok {
		return canonical.BadEndpoint("selected provider protocol is unsupported for messages adapter")
	}
	if kind != protocolkind.Messages {
		return canonical.BadEndpoint("selected provider protocol is not Messages")
	}
	if target.ProtocolKind != kind {
		return canonical.BadEndpoint("selected provider protocol kind does not match messages target")
	}
	return nil
}

func (e BackendAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string) error {
	if strings.TrimSpace(credentialRef) == "" { // swobu:io-string source=boundary
		return canonical.BadEndpoint("messages provider credential reference is required")
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
