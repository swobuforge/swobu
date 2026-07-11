package bedrock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

const (
	bedrockSigningService    = "bedrock"
	swobuCallerUAHeaderValue = "swobu/dev"
)

type ProviderIngressResolverAdapter struct {
	client *http.Client
}

var bedrockControlPlaneBaseURL = func(region string) string {
	return fmt.Sprintf("https://bedrock.%s.amazonaws.com", trimBedrockInput(region)) // swobu:io-string source=boundary
}

func NewExecutor(client *http.Client) ProviderIngressResolverAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return ProviderIngressResolverAdapter{client: client}
}

func NewRuntime(providerID profile.ProviderID, client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	executor := NewExecutor(client)
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         providerID,
		IngressResolver:    executor,
		CredentialProvider: credentials,
		ModelCatalogClient: executor,
	}
}

func (e ProviderIngressResolverAdapter) ResolveProviderIngress(ctx context.Context, req ports.ProviderRequest) (ports.ProviderIngress, error) {
	if trimBedrockInput(req.Request.Model()) == "" {
		return nil, canonical.BadRequest("canonical request is required")
	}
	if trimBedrockInput(req.Target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("bedrock provider base URL is required")
	}
	if err := validateBedrockRuntimeEndpoint(req.Target.BaseURL); err != nil {
		return nil, err
	}
	op, err := resolveBedrockOperation(req.Target.ProviderProtocol)
	if err != nil {
		return nil, err
	}
	client, err := e.runtimeClient(ctx, req.Target.BaseURL, req.Target.CredentialRef)
	if err != nil {
		return nil, err
	}
	if op.invokeModel {
		return e.executeInvokeModel(ctx, client, req, op.deliveryMode)
	}
	return e.executeConverse(ctx, client, req, op.deliveryMode)
}

func (e ProviderIngressResolverAdapter) ListModels(ctx context.Context, target exchange.RoutableTarget) ([]string, error) {
	mode, value := parseBedrockAuthMode(target.CredentialRef)
	if mode != bedrockAuthModeAWSProfile && mode != bedrockAuthModeAPIKeyEnv {
		return nil, canonical.BadEndpoint("bedrock auth mode is unsupported")
	}
	if trimBedrockInput(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("bedrock provider base URL is required")
	}
	if err := validateBedrockRuntimeEndpoint(target.BaseURL); err != nil {
		return nil, err
	}
	profile := ""
	if mode == bedrockAuthModeAWSProfile {
		profile = value
	}
	region, err := bedrockSigningRegion(ctx, mustParseURL(target.BaseURL), profile)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, bedrockControlPlaneBaseURL(region)+"/foundation-models", nil)
	if err != nil {
		return nil, canonical.BadEndpoint("bedrock provider model catalog request could not be built")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := applyBedrockAuth(ctx, target.CredentialRef, httpReq, nil); err != nil {
		return nil, err
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return nil, canonical.BadEndpoint("bedrock provider model catalog request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return nil, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		backendErr := httpedge.ReadBackendHTTPError(resp, target.BackendRef)
		logBedrockBackendDiagnostic("list_models", target, "/foundation-models", nil, backendErr)
		return nil, backendErr
	}
	var out struct {
		ModelSummaries []struct {
			ModelID string `json:"modelId"`
		} `json:"modelSummaries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, canonical.InternalError("backend model catalog could not be decoded")
	}
	models := make([]string, 0, len(out.ModelSummaries))
	for _, summary := range out.ModelSummaries {
		modelID := trimBedrockInput(summary.ModelID)
		if modelID != "" {
			models = append(models, modelID)
		}
	}
	slices.Sort(models)
	return models, nil
}

func (e ProviderIngressResolverAdapter) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	_, err := e.ListModels(ctx, target)
	return err
}

func (e ProviderIngressResolverAdapter) runtimeClient(ctx context.Context, baseURL string, credentialRef string) (*bedrockruntime.Client, error) {
	mode, value := parseBedrockAuthMode(credentialRef)
	region, err := bedrockSigningRegion(ctx, mustParseURL(baseURL), "")
	if err != nil {
		return nil, err
	}
	cfg, err := loadBedrockAWSConfig(ctx, region, mode, value)
	if err != nil {
		return nil, err
	}
	cfg.HTTPClient = e.client
	return bedrockruntime.NewFromConfig(cfg, func(o *bedrockruntime.Options) {
		o.BaseEndpoint = aws.String(baseURL)
		o.AuthSchemePreference = []string{"sigv4"}
		if mode == bedrockAuthModeAWSProfile {
			o.BearerAuthTokenProvider = nil
		}
	}), nil
}

func (e ProviderIngressResolverAdapter) executeConverse(ctx context.Context, client *bedrockruntime.Client, req ports.ProviderRequest, deliveryMode delivery.Mode) (ports.ProviderIngress, error) {
	modelID := bedrockModelFromRequest(req.Request)
	if trimBedrockInput(modelID) == "" {
		return nil, canonical.BadRequest("bedrock model id is required")
	}
	messages, err := bedrockMessagesFromRequest(req.Request)
	if err != nil {
		return nil, err
	}
	input := &bedrockruntime.ConverseInput{ModelId: aws.String(modelID), Messages: messages}
	if deliveryMode == delivery.Streaming {
		streamOut, err := client.ConverseStream(ctx, &bedrockruntime.ConverseStreamInput{ModelId: input.ModelId, Messages: input.Messages})
		if err != nil {
			return nil, classifyBedrockSDKError(err)
		}
		text, usage, stop := collectConverseStream(streamOut)
		output := mustConversationOutput(modelID, text, stop, usage)
		return synthesizeBedrockProviderSemanticEventsResult(req.ExchangeID, req.Target.ProtocolKind, output)
	}
	out, err := client.Converse(ctx, input)
	if err != nil {
		return nil, classifyBedrockSDKError(err)
	}
	text, stop, usage := decodeConverseOutput(out)
	output := mustConversationOutput(modelID, text, stop, usage)
	return synthesizeBedrockProviderSemanticEventsResult(req.ExchangeID, req.Target.ProtocolKind, output)
}

func (e ProviderIngressResolverAdapter) executeInvokeModel(ctx context.Context, client *bedrockruntime.Client, req ports.ProviderRequest, deliveryMode delivery.Mode) (ports.ProviderIngress, error) {
	modelID := trimBedrockInput(req.Request.Model())
	if modelID == "" {
		return nil, canonical.BadRequest("bedrock model id is required")
	}
	payload, err := json.Marshal(map[string]any{"inputText": bedrockPromptText(req.Request)})
	if err != nil {
		return nil, canonical.BadRequest("bedrock invoke_model payload could not be encoded")
	}
	if deliveryMode == delivery.Streaming {
		out, err := client.InvokeModelWithResponseStream(ctx, &bedrockruntime.InvokeModelWithResponseStreamInput{
			ModelId:     aws.String(modelID),
			Body:        payload,
			ContentType: aws.String("application/json"),
			Accept:      aws.String("application/json"),
		})
		if err != nil {
			return nil, classifyBedrockSDKError(err)
		}
		raw := collectInvokeModelResponseStream(out)
		decoded, err := decodeInvokeModelBuffered(raw)
		if err != nil {
			return nil, err
		}
		return synthesizeBedrockProviderSemanticEventsResult(req.ExchangeID, req.Target.ProtocolKind, decoded)
	}
	out, err := client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(modelID),
		Body:        payload,
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
	})
	if err != nil {
		return nil, classifyBedrockSDKError(err)
	}
	decoded, err := decodeInvokeModelBuffered(out.Body)
	if err != nil {
		return nil, err
	}
	return synthesizeBedrockProviderSemanticEventsResult(req.ExchangeID, req.Target.ProtocolKind, decoded)
}

func synthesizeBedrockProviderSemanticEventsResult(exchangeID string, kind protocolkind.ProtocolKind, output canonical.CanonicalOutput) (ports.ProviderIngress, error) {
	if err := validateBedrockSyntheticProtocol(kind); err != nil {
		return nil, err
	}
	envelope, err := canonical.EventReaderFromCanonicalOutput(exchangeID, output)
	if err != nil {
		return nil, canonical.InternalError("bedrock semantic event synthesis failed")
	}
	return carrier.CanonicalEventStream{Events: envelope}, nil
}

func validateBedrockSyntheticProtocol(kind protocolkind.ProtocolKind) error {
	switch kind {
	case protocolkind.ChatCompletions, protocolkind.Completions:
		return nil
	default:
		return canonical.UnsupportedOperation("bedrock synthetic response protocol is not implemented")
	}
}

func bedrockPromptText(request canonical.CanonicalRequest) string {
	var out strings.Builder
	for _, item := range request.Items() {
		if item.Kind != canonical.ItemKindText {
			continue
		}
		out.WriteString(item.Text)
	}
	return out.String()
}
