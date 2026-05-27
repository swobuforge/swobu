// selection, wire realization, and response decoding in one outbound seam.
package openaifamily

import (
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	core "github.com/swobuforge/swobu/internal/adapters/wire/primitives"
	protocolregistry "github.com/swobuforge/swobu/internal/adapters/wire/protocolregistry"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
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
func (e ProviderExecutorAdapter) Execute(ctx context.Context, req ports.ProviderRequest) (ports.ProviderResponse, error) {
	if strings.TrimSpace(req.Request.Model()) == "" { // swobu:io-string source=boundary
		return ports.ProviderResponse{}, canonical.BadRequest("canonical request is required")
	}
	if strings.TrimSpace(req.Target.BaseURL) == "" { // swobu:io-string source=boundary
		return ports.ProviderResponse{}, canonical.BadEndpoint("OpenAI-family provider base URL is required")
	}
	if requiresExplicitCredentialRef(req.Target.ProviderID(), req.Target.BaseURL, req.Target.CredentialRef) {
		return ports.ProviderResponse{}, canonical.BadEndpoint(providerCredentialRequiredMessage(req.Target.ProviderID()))
	}

	dispatch, streaming, err := resolveProviderProtocolDispatch(req.Target)
	if err != nil {
		return ports.ProviderResponse{}, err
	}

	encodePatches, cacheWarnings, err := e.buildEncodePatches(req.Target.ProviderID(), req.Request)
	if err != nil {
		return ports.ProviderResponse{}, err
	}
	wireReq, err := realizeProtocolRequestWire(req.Request, dispatch.kind, streaming)
	if err != nil {
		return ports.ProviderResponse{}, err
	}
	wireReq, err = RequestPatcher{}.Patch(wireReq, dispatch.kind, streaming, encodePatches)
	if err != nil {
		return ports.ProviderResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, wireReq.Method, httpedge.JoinBaseURLAndPath(req.Target.BaseURL, wireReq.Path), wireReq.Body)
	if err != nil {
		return ports.ProviderResponse{}, canonical.BadEndpoint("OpenAI-family provider request could not be built")
	}
	if wireReq.HasBody {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)

	if err := e.applyCredential(ctx, httpReq, req.Target.ProviderID(), req.Target.CredentialRef); err != nil {
		return ports.ProviderResponse{}, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return ports.ProviderResponse{}, canonical.BadEndpoint("OpenAI-family provider request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
		return ports.ProviderResponse{}, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	if resp.StatusCode >= 400 {
		defer func() {
			_ = resp.Body.Close()
		}()
		backendErr := httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
		return ports.ProviderResponse{}, classifyBackendError(backendErr)
	}

	if streaming {
		streamResp, streamWarnings, err := decodeStreamByKind(e.profile, dispatch.kind, resp.Body, "provider_stream:"+string(dispatch.kind), resp.Header.Clone())
		if err != nil {
			return ports.ProviderResponse{}, err
		}
		streamResponse := streamResp
		allWarnings := append([]ports.DegradationWarning(nil), cacheWarnings...)
		allWarnings = append(allWarnings, streamWarnings...)
		if len(allWarnings) > 0 {
			streamResponse = streamResponse.WithMetadata(ports.ProviderResponseMetadata{DegradationWarnings: allWarnings})
		}
		return streamResponse, nil
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ports.ProviderResponse{}, canonical.InternalError("backend success response could not be read")
	}
	output, decodeWarnings, err := decodeBufferedByKind(e.profile, dispatch.kind, raw, resp.Header.Clone())
	if err != nil {
		return ports.ProviderResponse{}, err
	}
	response := ports.NewBufferedProviderResponse(output)
	allWarnings := append([]ports.DegradationWarning(nil), cacheWarnings...)
	allWarnings = append(allWarnings, decodeWarnings...)
	if len(allWarnings) > 0 {
		response = response.WithMetadata(ports.ProviderResponseMetadata{DegradationWarnings: allWarnings})
	}
	return response, nil
}

// applyCredential keeps auth resolution at the provider edge so canonicals and
// app orchestration never need to know provider token mechanics.
func (e ProviderExecutorAdapter) applyCredential(ctx context.Context, req *http.Request, providerSpec string, credentialRef string) error {
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
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

type providerProtocolDispatchRecord struct {
	kind protocolkind.ProtocolKind
}

func resolveProviderProtocolDispatch(target ports.RoutableTarget) (providerProtocolDispatchRecord, bool, error) {
	providerProtocol := strings.TrimSpace(target.ProviderProtocol) // swobu:io-string source=boundary
	if providerProtocol == "" || providerProtocol == providercatalog.ProviderProtocolAuto {
		return providerProtocolDispatchRecord{}, false, canonical.BadEndpoint("OpenAI-family provider protocol must be concrete")
	}
	kind, _, ok := providercatalog.ProviderProtocolKindAndFrame(target.ProviderID(), providerProtocol)
	if !ok {
		return providerProtocolDispatchRecord{}, false, canonical.BadEndpoint("selected provider protocol is unsupported")
	}
	streaming := strings.HasSuffix(providerProtocol, "_stream")
	return providerProtocolDispatchRecord{kind: kind}, streaming, nil
}

func realizeProtocolRequestWire(req canonical.CanonicalRequest, kind protocolkind.ProtocolKind, streaming bool) (core.WireRequest, error) {
	codec, err := protocolregistry.ForProtocolKind(kind)
	if err != nil {
		if kind == protocolkind.Messages {
			return core.WireRequest{}, canonical.UnsupportedOperation("OpenAI-family provider does not implement the messages protocol")
		}
		return core.WireRequest{}, err
	}
	return codec.EncodeRequest(req, streaming)
}

func requiresExplicitCredentialRef(providerSpec string, baseURL string, credentialRef string) bool {
	if strings.TrimSpace(credentialRef) != "" { // swobu:io-string source=boundary
		return false
	}
	return providercatalog.RequiresCredential(strings.TrimSpace(providerSpec), strings.TrimSpace(baseURL)) // swobu:io-string source=boundary
}

func providerCredentialRequiredMessage(providerSpec string) string {
	providerID, ok := providercatalog.ParseProviderID(strings.TrimSpace(providerSpec)) // swobu:io-string source=boundary
	if !ok {
		return "provider credential reference is required"
	}
	return string(providerID) + " provider credential reference is required"
}

func (e ProviderExecutorAdapter) buildEncodePatches(providerIDRaw string, req canonical.CanonicalRequest) ([]core.WirePatch, []ports.DegradationWarning, error) {
	providerID, ok := providercatalog.ParseProviderID(strings.TrimSpace(providerIDRaw)) // swobu:io-string source=boundary
	if !ok {
		return nil, nil, canonical.BadEndpoint("provider id is unsupported for OpenAI-family adapter runtime")
	}
	if providerID != e.profile.ProviderID() {
		return nil, nil, canonical.BadEndpoint("provider policy is unsupported for OpenAI-family adapter runtime")
	}
	patches, warnings := e.profile.BuildEncodePatches(req)
	return patches, warnings, nil
}
