package bedrock

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
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

const swobuCallerUAHeaderValue = "swobu/dev"

type BackendAdapter struct {
	client         *http.Client
	credentials    providersruntime.CredentialProvider
	callerIdentity func(context.Context, aws.Config) (*sts.GetCallerIdentityOutput, error)
}

func NewExecutor(client *http.Client) BackendAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return BackendAdapter{client: client, callerIdentity: defaultCallerIdentity}
}

func NewRuntime(providerID profile.ProviderID, client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	executor := NewExecutor(client)
	executor.credentials = credentials
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         providerID,
		BackendResolver:    executor,
		TargetSupport:      provider.TargetSupportFunc(provider.UnknownTargetSupport),
		CredentialProvider: credentials,
		Discovery:          executor,
	}
}

// ResolveBackend composes one exact Bedrock Mantle backend.
func (e BackendAdapter) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	codec := provider.Codec(protocolcodec.Codec{Protocol: target.ProtocolKind})
	switch target.ProtocolKind {
	case protocolkind.Messages:
		codec = mantleMessagesCodec{Codec: codec}
	case protocolkind.Responses:
		// RFC G2 §7.6: the Bedrock Responses reasoning-replay identity seam. It is
		// an identity decorator today; the named site where a proven normalization
		// of the reasoning wire id would land (see mantleResponsesCodec).
		codec = mantleResponsesCodec{Codec: codec}
	}
	backend := provider.Backend{Target: target.Clone(), Codec: codec, Transport: provider.BindTransport(target, e.Send)}
	if err := backend.Validate(); err != nil {
		return provider.Backend{}, err
	}
	return backend, nil
}

// Send performs Bedrock Mantle transport over a final provider document.
func (e BackendAdapter) Send(ctx context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
	if strings.TrimSpace(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, provider.AttemptNotDispatched(canonical.BadEndpoint("bedrock provider base URL is required"))
	}
	if err := validateBedrockMantleEndpoint(target.BaseURL, target.BedrockRegion()); err != nil {
		return nil, provider.AttemptNotDispatched(err)
	}
	// Route through the single Bedrock endpoint boundary so endpoint and protocol
	// meet once: it strips a pasted terminal operation exactly once (no doubled
	// /responses), derives the request URL from one parse, and preserves any
	// reverse-proxy prefix. The Bedrock adapter owns no URL parsing of its own.
	resolution, err := profile.ResolveBedrockEndpoint(target.BaseURL, target.BedrockRegion(), target.ProtocolKind)
	if err != nil {
		return nil, provider.AttemptNotDispatched(provider.NewIncompatibleTarget(err.Error()))
	}
	if resolution.RequestURL == "" {
		return nil, provider.AttemptNotDispatched(provider.NewIncompatibleTarget("bedrock provider protocol has no request path"))
	}
	requestURL := resolution.RequestURL

	wireReqCarrier := doc
	if wireReqCarrier.IsEmpty() {
		return nil, provider.AttemptNotDispatched(canonical.InternalError("provider request document is required"))
	}
	wireReqBody := wireReqCarrier.RawBytes()
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		requestURL,
		bytes.NewReader(wireReqBody),
	)
	if err != nil {
		return nil, provider.AttemptNotDispatched(canonical.BadEndpoint("bedrock provider request could not be built"))
	}
	if len(wireReqBody) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if target.ProtocolKind == protocolkind.Messages {
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	}
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)

	if err := applyBedrockAuth(ctx, e.credentials, target.CredentialRef, httpReq, wireReqBody, target.BedrockRegion()); err != nil {
		return nil, provider.AttemptNotDispatched(err)
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, provider.TransportFailure(ctx, canonical.BadEndpoint("bedrock provider request failed before backend response"))
	}
	decodedResp, err := httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return nil, provider.AttemptMayHaveExecuted(canonical.InternalError("backend response content encoding is unsupported or invalid"))
	}
	resp = decodedResp
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		backendErr := httpedge.ReadBackendHTTPError(resp, target.TargetID)
		// The resolver owns URL derivation; recompute the operation label for the
		// diagnostic from the protocol kind so this call site owns no URL parsing.
		requestPath, _ := profile.ProviderRequestPath(string(profile.ProviderSpecBedrock), target.ProtocolKind)
		logBedrockBackendDiagnostic("execute", target, requestPath, backendErr)
		logBedrockRequestValidationDiagnostic(target, requestPath, backendErr, wireReqBody)
		return nil, provider.AttemptMayHaveExecuted(backendErr)
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
		target.ProtocolKind,
		"application/json",
		resp.Header.Clone(),
		raw,
		carrier.Meta{},
	)}, nil
}

var _ provider.BackendResolver = BackendAdapter{}

func (e BackendAdapter) ListDeployments(ctx context.Context, target provider.TargetSnapshot) ([]profile.ProviderDeploymentRecord, error) {
	if strings.TrimSpace(target.BaseURL) != "" {
		if err := validateBedrockMantleEndpoint(target.BaseURL, target.BedrockRegion()); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(target.BedrockRegion()) == "" {
		return nil, canonical.BadEndpoint("bedrock signing region is required")
	}
	region := bedrockSigningRegionForTarget(target)
	resolved, err := resolveBedrockAuth(ctx, e.credentials, target.CredentialRef, region)
	if err != nil {
		return nil, err
	}
	return e.listDeploymentsWithAuth(ctx, target, resolved, region)
}

func (e BackendAdapter) listDeploymentsWithAuth(ctx context.Context, target provider.TargetSnapshot, resolved resolvedBedrockAuthState, region string) ([]profile.ProviderDeploymentRecord, error) {

	// Catalog discovery is regional service connectivity, independent from the
	// model-specific inference namespace authored on the target.
	catalogURL := profile.BedrockCatalogURL(target.BedrockRegion())
	if catalogURL == "" {
		return nil, canonical.BadEndpoint("bedrock provider model catalog requires a region")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, catalogURL, nil)
	if err != nil {
		return nil, canonical.BadEndpoint("bedrock provider model catalog request could not be built")
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := applyResolvedBedrockAuth(ctx, resolved, httpReq, nil, region); err != nil {
		return nil, err
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("bedrock provider model catalog request failed before backend response")
	}
	decodedResp, err := httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return nil, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	resp = decodedResp
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		backendErr := httpedge.ReadBackendHTTPError(resp, target.TargetID)
		logBedrockBackendDiagnostic("list_models", target, "/models", backendErr)
		return nil, backendErr
	}
	models, err := modelcatalogopenai.DecodeModelIDs(resp.Body)
	if err != nil {
		return nil, canonical.InternalError("backend model catalog could not be decoded")
	}
	supportedProtocols := profile.ConcreteProviderProtocolsForSpec(string(profile.ProviderSpecBedrock))
	out := make([]profile.ProviderDeploymentRecord, 0, len(models))
	for _, modelID := range models {
		out = append(out, profile.NewProviderDeployment(
			modelID,
			modelID,
			string(profile.ProviderSpecBedrock),
			"",
			string(profile.ProviderSpecBedrock),
			supportedProtocols,
			"",
		))
	}
	return out, nil
}

type targetProbeDiagnostic struct {
	Authentication string             `json:"authentication"`
	FailureStage   string             `json:"failure_stage,omitempty"`
	Error          string             `json:"error,omitempty"`
	AWSIdentity    *awsIdentityStatus `json:"aws_identity,omitempty"`
}

type awsIdentityStatus struct {
	State   string `json:"state"`
	Account string `json:"account,omitempty"`
	ARN     string `json:"arn,omitempty"`
	Error   string `json:"error,omitempty"`
}

func defaultCallerIdentity(ctx context.Context, cfg aws.Config) (*sts.GetCallerIdentityOutput, error) {
	return sts.NewFromConfig(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
}

// ProbeTarget resolves authentication once, uses it for the catalog request,
// and treats STS caller identity as optional enrichment even when the catalog
// fails. This preserves fresh identity evidence for the refresh-identity job.
func (e BackendAdapter) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	region := bedrockSigningRegionForTarget(target)
	if region == "" {
		return provider.TargetProbeResult{}, canonical.BadEndpoint("bedrock signing region is required")
	}
	resolved, err := resolveBedrockAuth(ctx, e.credentials, target.CredentialRef, region)
	if err != nil {
		authentication := bedrockAuthAWSIdentity.String()
		if strings.TrimSpace(target.CredentialRef) != "" {
			authentication = bedrockAuthTargetAPIKey.String()
		}
		raw, marshalErr := json.Marshal(targetProbeDiagnostic{Authentication: authentication, FailureStage: "authentication", Error: err.Error()})
		if marshalErr != nil {
			return provider.TargetProbeResult{}, err
		}
		return provider.TargetProbeResult{Diagnostics: raw}, err
	}
	deployments, catalogErr := e.listDeploymentsWithAuth(ctx, target, resolved, region)
	diagnostics := targetProbeDiagnostic{Authentication: resolved.kind.String()}
	if resolved.config != nil {
		identity := awsIdentityStatus{}
		out, identityErr := e.callerIdentity(ctx, *resolved.config)
		if identityErr != nil {
			identity = awsIdentityStatus{State: "identity_probe_failed", Error: identityErr.Error()}
		} else {
			identity = awsIdentityStatus{State: "resolved", Account: aws.ToString(out.Account), ARN: aws.ToString(out.Arn)}
		}
		diagnostics.AWSIdentity = &identity
	}
	if catalogErr != nil {
		diagnostics.FailureStage = "catalog"
		raw, marshalErr := json.Marshal(diagnostics)
		if marshalErr != nil {
			return provider.TargetProbeResult{}, catalogErr
		}
		return provider.TargetProbeResult{Diagnostics: raw}, catalogErr
	}
	rawDiagnostics, err := json.Marshal(diagnostics)
	if err != nil {
		return provider.TargetProbeResult{}, canonical.InternalError("bedrock target diagnostics could not be encoded")
	}
	return provider.TargetProbeResult{Deployments: deployments, Diagnostics: rawDiagnostics}, nil
}
