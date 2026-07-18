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
	providercompat "github.com/swobuforge/swobu/internal/adapters/outbound/providers/providercompat"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
)

const swobuCallerUAHeaderValue = "swobu/dev"

type ProviderIngressResolverAdapter struct {
	client         *http.Client
	credentials    providersruntime.CredentialProvider
	callerIdentity func(context.Context, aws.Config) (*sts.GetCallerIdentityOutput, error)
}

func NewExecutor(client *http.Client) ProviderIngressResolverAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return ProviderIngressResolverAdapter{client: client, callerIdentity: defaultCallerIdentity}
}

func NewRuntime(providerID profile.ProviderID, client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	executor := NewExecutor(client)
	executor.credentials = credentials
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         providerID,
		ProviderExecutor:   executor,
		CredentialProvider: credentials,
		Discovery:          executor,
	}
}

func (e ProviderIngressResolverAdapter) ResolveProviderIngress(ctx context.Context, req exchange.ProviderRequest) (exchange.ProviderIngress, error) {
	if strings.TrimSpace(req.Request.Model()) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadRequest("canonical request is required")
	}
	if strings.TrimSpace(req.Target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("bedrock provider base URL is required")
	}
	if err := validateBedrockMantleEndpoint(req.Target.BaseURL); err != nil {
		return nil, err
	}
	if err := req.Contract.ProviderDelivery.Validate(); err != nil {
		return nil, canonical.UnsupportedDelivery("bedrock provider delivery is unsupported")
	}
	if req.Contract.ProviderDelivery.IsStreaming() && req.Contract.ProviderDelivery.Framing != delivery.FramingSSE {
		return nil, canonical.UnsupportedDelivery("bedrock provider does not implement the requested delivery framing")
	}

	path, err := profile.ProviderRequestPath(req.Target.ProviderID(), req.Target.ProtocolKind)
	if err != nil {
		return nil, err
	}

	wireReqCarrier := req.RequestDocument
	if wireReqCarrier.IsEmpty() {
		return nil, canonical.InternalError("provider request document is required")
	}
	if err := providercompat.EmitStructuredOutputDecisions(ctx, req.EffectSink, req.ExchangeID, req.Target.ProviderID(), req.Target.ProtocolKind, req.Request.OutputFormat()); err != nil {
		return nil, err
	}
	if err := providercompat.EmitToolSchemaStrictDecision(ctx, req.EffectSink, req.ExchangeID, req.Target.ProviderID(), req.Target.ProtocolKind, req.Request.Tools(), req.Target.ProtocolKind != protocolkind.Messages); err != nil {
		return nil, err
	}

	wireReqBody := wireReqCarrier.RawBytes()
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		httpedge.JoinBaseURLAndPath(req.Target.BaseURL, path),
		bytes.NewReader(wireReqBody),
	)
	if err != nil {
		return nil, canonical.BadEndpoint("bedrock provider request could not be built")
	}
	if len(wireReqBody) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)

	if err := applyBedrockAuth(ctx, e.credentials, req.Target.CredentialRef, httpReq, wireReqBody); err != nil {
		return nil, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("bedrock provider request failed before backend response")
	}
	decodedResp, err := httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return nil, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	resp = decodedResp
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		backendErr := httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
		logBedrockBackendDiagnostic("execute", req.Target, path, wireReqBody, backendErr)
		return nil, backendErr
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

func (e ProviderIngressResolverAdapter) ListDeployments(ctx context.Context, target exchange.RoutableTarget) ([]profile.ProviderDeploymentRecord, error) {
	if strings.TrimSpace(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("bedrock provider base URL is required")
	}
	if err := validateBedrockMantleEndpoint(target.BaseURL); err != nil {
		return nil, err
	}
	_, region := bedrockEndpointClassAndRegion(target.BaseURL)
	resolved, err := resolveBedrockAuth(ctx, e.credentials, target.CredentialRef, region)
	if err != nil {
		return nil, err
	}
	return e.listDeploymentsWithAuth(ctx, target, resolved, region)
}

func (e ProviderIngressResolverAdapter) listDeploymentsWithAuth(ctx context.Context, target exchange.RoutableTarget, resolved resolvedBedrockAuthState, region string) ([]profile.ProviderDeploymentRecord, error) {

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, httpedge.JoinBaseURLAndPath(target.BaseURL, "/models"), nil)
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
		backendErr := httpedge.ReadBackendHTTPError(resp, target.BackendRef)
		logBedrockBackendDiagnostic("list_models", target, "/models", nil, backendErr)
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
func (e ProviderIngressResolverAdapter) ProbeTarget(ctx context.Context, target exchange.RoutableTarget) (exchange.TargetProbeResult, error) {
	region, err := bedrockSigningRegion(mustParseURL(target.BaseURL))
	if err != nil {
		return exchange.TargetProbeResult{}, err
	}
	resolved, err := resolveBedrockAuth(ctx, e.credentials, target.CredentialRef, region)
	if err != nil {
		authentication := bedrockAuthAWSIdentity.String()
		if strings.TrimSpace(target.CredentialRef) != "" {
			authentication = bedrockAuthTargetAPIKey.String()
		}
		raw, marshalErr := json.Marshal(targetProbeDiagnostic{Authentication: authentication, FailureStage: "authentication", Error: err.Error()})
		if marshalErr != nil {
			return exchange.TargetProbeResult{}, err
		}
		return exchange.TargetProbeResult{Diagnostics: raw}, err
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
			return exchange.TargetProbeResult{}, catalogErr
		}
		return exchange.TargetProbeResult{Diagnostics: raw}, catalogErr
	}
	rawDiagnostics, err := json.Marshal(diagnostics)
	if err != nil {
		return exchange.TargetProbeResult{}, canonical.InternalError("bedrock target diagnostics could not be encoded")
	}
	return exchange.TargetProbeResult{Deployments: deployments, Diagnostics: rawDiagnostics}, nil
}
