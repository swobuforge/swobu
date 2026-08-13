package gemini

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"cloud.google.com/go/auth"
	"cloud.google.com/go/auth/credentials"
	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// NewRuntime composes Gemini's private native Interactions adapter. It uses
// direct HTTP so the provider owns its protocol, authentication, and stream
// lifecycle without borrowing Google's OpenAI facade or an SDK.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	return newRuntime(client, credentials, detectDefaultADC)
}

type adcCredentials interface {
	Token(context.Context) (*auth.Token, error)
	QuotaProjectID(context.Context) (string, error)
}

type adcDetector func(context.Context) (adcCredentials, error)

func detectDefaultADC(context.Context) (adcCredentials, error) {
	return credentials.DetectDefault(&credentials.DetectOptions{Scopes: []string{
		"https://www.googleapis.com/auth/cloud-platform",
		"https://www.googleapis.com/auth/generative-language.retriever",
	}})
}

func newRuntime(client *http.Client, credentialProvider providersruntime.CredentialProvider, detectADC adcDetector) providersruntime.ProviderRuntimeBundle {
	if client == nil {
		client = http.DefaultClient
	}
	runtime := &geminiRuntime{client: client, credentials: credentialProvider, detectADC: detectADC}
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         profile.ProviderSpecGemini,
		BackendResolver:    backendResolver{runtime: runtime},
		TargetSupport:      provider.TargetSupportFunc(provider.UnknownTargetSupport),
		CredentialProvider: credentialProvider,
		Discovery:          discovery{runtime: runtime},
	}
}

type geminiRuntime struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
	detectADC   adcDetector
	adcMu       sync.Mutex
	adc         adcCredentials
}

type backendResolver struct{ runtime *geminiRuntime }

func (r backendResolver) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	if err := validateTarget(target); err != nil {
		return provider.Backend{}, err
	}
	backend := provider.Backend{
		Target:    target.Clone(),
		Codec:     codec{},
		Transport: provider.BindTransport(target, r.runtime.send),
	}
	return backend, backend.Validate()
}

// send performs Gemini's provider-only POST operation over an already encoded
// Interactions document. Canonical request state is intentionally unavailable
// here, so transport cannot create a second request-lowering path.
func (r *geminiRuntime) send(ctx context.Context, target provider.TargetSnapshot, document carrier.Document) (provider.Ingress, error) {
	if err := validateTarget(target); err != nil {
		return nil, provider.AttemptNotDispatched(err)
	}
	if strings.TrimSpace(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, provider.AttemptNotDispatched(canonical.BadEndpoint("Gemini provider base URL is required"))
	}
	if document.Family != protocolkind.Interactions || document.Media != "application/json" || document.IsEmpty() {
		return nil, provider.AttemptNotDispatched(canonical.InternalError("Gemini Interactions request document is invalid"))
	}
	auth, err := r.resolveAuth(ctx, target)
	if err != nil {
		return nil, provider.AttemptNotDispatched(err)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, httpedge.JoinBaseURLAndPath(target.BaseURL, "/interactions"), bytes.NewReader(document.RawBytes()))
	if err != nil {
		return nil, provider.AttemptNotDispatched(canonical.BadEndpoint("Gemini Interactions request could not be built"))
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	httpRequest.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	applyAuth(httpRequest, auth)

	response, err := r.client.Do(httpRequest)
	if err != nil {
		return nil, provider.TransportFailure(ctx, fmt.Errorf("%w: %w", canonical.BadEndpoint("Gemini Interactions request failed before backend response"), err))
	}
	response, err = httpedge.DecodeHTTPResponseContentEncoding(response)
	if err != nil {
		defer func() { _ = response.Body.Close() }()
		return nil, provider.AttemptMayHaveExecuted(canonical.InternalError("Gemini backend response content encoding is unsupported or invalid"))
	}
	if response.StatusCode >= http.StatusBadRequest {
		defer func() { _ = response.Body.Close() }()
		return nil, provider.AttemptMayHaveExecuted(httpedge.ReadBackendHTTPError(response, target.TargetID))
	}
	if !httpedge.IsEventStreamContentType(response.Header.Get("Content-Type")) {
		return nil, provider.AttemptMayHaveExecuted(httpedge.ReadUnexpectedStreamingResponse(response, target.TargetID))
	}
	return provider.StreamIngress{Stream: carrier.ByteStream{
		Header:    response.Header.Clone(),
		MediaType: response.Header.Get("Content-Type"),
		Body:      response.Body,
	}}, nil
}

type authKind uint8

const (
	authAPIKey authKind = iota + 1
	authADC
)

// resolvedAuth is the closed Gemini HTTP authentication projection. Its kind
// makes sending API-key and ADC credentials together unrepresentable.
type resolvedAuth struct {
	kind         authKind
	credential   string
	quotaProject string
}

func (r *geminiRuntime) resolveAuth(ctx context.Context, target provider.TargetSnapshot) (resolvedAuth, error) {
	if strings.TrimSpace(target.CredentialRef) == "" { // swobu:io-string source=boundary
		return r.resolveADC(ctx)
	}
	if r.credentials == nil {
		return resolvedAuth{}, canonical.BadEndpoint("credential resolver is not configured")
	}
	token, err := r.credentials.ResolveCredential(ctx, target.ProviderID(), target.CredentialRef)
	if err != nil {
		return resolvedAuth{}, canonical.BadEndpoint("credential reference could not be resolved")
	}
	if strings.TrimSpace(token) == "" { // swobu:io-string source=boundary
		return resolvedAuth{}, canonical.BadEndpoint("credential reference resolved to an empty token")
	}
	return resolvedAuth{kind: authAPIKey, credential: token}, nil
}

func (r *geminiRuntime) resolveADC(ctx context.Context) (resolvedAuth, error) {
	r.adcMu.Lock()
	adc := r.adc
	if adc == nil {
		if r.detectADC == nil {
			r.adcMu.Unlock()
			return resolvedAuth{}, canonical.BadEndpoint("Gemini Google identity (ADC) is unavailable")
		}
		detected, err := r.detectADC(ctx)
		if err != nil || detected == nil {
			r.adcMu.Unlock()
			return resolvedAuth{}, canonical.BadEndpoint("Gemini Google identity (ADC) is unavailable")
		}
		adc = detected
		r.adc = detected
	}
	r.adcMu.Unlock()
	token, err := adc.Token(ctx)
	if err != nil || token == nil || strings.TrimSpace(token.Value) == "" {
		return resolvedAuth{}, canonical.BadEndpoint("Gemini Google identity (ADC) is unavailable")
	}
	quotaProject, quotaErr := adc.QuotaProjectID(ctx)
	if quotaErr != nil {
		quotaProject = ""
	}
	return resolvedAuth{kind: authADC, credential: token.Value, quotaProject: strings.TrimSpace(quotaProject)}, nil
}

func applyAuth(request *http.Request, resolved resolvedAuth) {
	request.Header.Del("x-goog-api-key")
	request.Header.Del("Authorization")
	request.Header.Del("x-goog-user-project")
	switch resolved.kind {
	case authAPIKey:
		request.Header.Set("x-goog-api-key", resolved.credential)
	case authADC:
		request.Header.Set("Authorization", "Bearer "+resolved.credential)
		if resolved.quotaProject != "" {
			request.Header.Set("x-goog-user-project", resolved.quotaProject)
		}
	}
}

func validateTarget(target provider.TargetSnapshot) error {
	if target.ProviderID() != string(profile.ProviderSpecGemini) {
		return canonical.BadEndpoint("selected provider does not match Gemini runtime")
	}
	if target.ProtocolKind != protocolkind.Interactions || target.ProviderProtocol != "interactions_stream" || target.SelectedFrame != profile.FrameSSEEvent {
		return canonical.BadEndpoint("Gemini target must use interactions_stream")
	}
	if err := target.ValidateExecutionProtocol(); err != nil {
		return canonical.BadEndpoint(err.Error())
	}
	return nil
}

var _ provider.BackendResolver = backendResolver{}
