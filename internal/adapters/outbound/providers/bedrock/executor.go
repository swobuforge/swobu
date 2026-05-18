package bedrock

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	bedrocksdk "github.com/aws/aws-sdk-go-v2/service/bedrock"
	bedrocktypes "github.com/aws/aws-sdk-go-v2/service/bedrock/types"
	modelcatalogopenaicompat "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalogprotocols/openaicompat"
	"github.com/swobuforge/swobu/internal/adapters/outbound/protocols"
	chatcompletions "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/chat_completions"
	completions "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/completions"
	responses "github.com/swobuforge/swobu/internal/adapters/outbound/protocols/responses"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/httpedge"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/domain/providercatalog"
	"github.com/swobuforge/swobu/internal/ports"
)

const (
	bedrockSigningService    = "bedrock"
	swobuCallerUAHeaderValue = "swobu/dev"
)

type ProviderExecutorAdapter struct {
	client *http.Client
}

type protocolCodecDispatchSpec struct {
	realize        func(req canonical.CanonicalRequest, streaming bool) (protocols.WireRequest, error)
	decodeBuffered func(raw []byte) (ports.ProviderResponse, error)
	decodeStream   func(body io.ReadCloser) (ports.ProviderResponse, error)
}

var protocolDispatchTable = map[protocolkind.ProtocolKind]protocolCodecDispatchSpec{
	protocolkind.ChatCompletions: {
		realize: func(req canonical.CanonicalRequest, streaming bool) (protocols.WireRequest, error) {
			return chatcompletions.EncodeRequest(req, streaming)
		},
		decodeBuffered: func(raw []byte) (ports.ProviderResponse, error) {
			result, err := chatcompletions.DecodeResponseBuffered(raw)
			if err != nil {
				return ports.ProviderResponse{}, err
			}
			return ports.NewBufferedProviderResponse(result), nil
		},
		decodeStream: func(body io.ReadCloser) (ports.ProviderResponse, error) {
			return ports.NewEnvelopeStreamingProviderResponse(chatcompletions.DecodeResponseStream(body, "provider_stream:chat_completions")), nil
		},
	},
	protocolkind.Responses: {
		realize: func(req canonical.CanonicalRequest, streaming bool) (protocols.WireRequest, error) {
			return responses.EncodeRequest(req, streaming)
		},
		decodeBuffered: func(raw []byte) (ports.ProviderResponse, error) {
			result, err := responses.DecodeResponseBuffered(raw)
			if err != nil {
				return ports.ProviderResponse{}, err
			}
			return ports.NewBufferedProviderResponse(result), nil
		},
		decodeStream: func(body io.ReadCloser) (ports.ProviderResponse, error) {
			return ports.NewEnvelopeStreamingProviderResponse(responses.DecodeResponseStream(body, "provider_stream:responses")), nil
		},
	},
	protocolkind.Completions: {
		realize: func(req canonical.CanonicalRequest, streaming bool) (protocols.WireRequest, error) {
			return completions.EncodeRequest(req, streaming)
		},
		decodeBuffered: func(raw []byte) (ports.ProviderResponse, error) {
			result, err := completions.DecodeResponseBuffered(raw)
			if err != nil {
				return ports.ProviderResponse{}, err
			}
			return ports.NewBufferedProviderResponse(result), nil
		},
		decodeStream: func(body io.ReadCloser) (ports.ProviderResponse, error) {
			return ports.NewEnvelopeStreamingProviderResponse(completions.DecodeResponseStream(body, "provider_stream:completions")), nil
		},
	},
}

var bedrockListFoundationModelIDs = func(ctx context.Context, cfg aws.Config) ([]string, error) {
	client := bedrocksdk.NewFromConfig(cfg)
	out, err := client.ListFoundationModels(ctx, &bedrocksdk.ListFoundationModelsInput{
		ByOutputModality: bedrocktypes.ModelModalityText,
	})
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(out.ModelSummaries))
	for _, summary := range out.ModelSummaries {
		modelID := trimBedrockInput(aws.ToString(summary.ModelId))
		if modelID != "" {
			models = append(models, modelID)
		}
	}
	slices.Sort(models)
	return models, nil
}

func NewExecutor(client *http.Client) ProviderExecutorAdapter {
	if client == nil {
		client = http.DefaultClient
	}
	return ProviderExecutorAdapter{client: client}
}

func NewRuntime(providerID providercatalog.ProviderID, client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	executor := NewExecutor(client)
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         providerID,
		Executor:           executor,
		CredentialProvider: credentials,
		ModelCatalogClient: executor,
	}
}

func (e ProviderExecutorAdapter) Execute(ctx context.Context, req ports.ProviderRequest) (ports.ProviderResponse, error) {
	if req.Request == nil {
		return ports.ProviderResponse{}, canonical.BadRequest("canonical request is required")
	}
	if trimBedrockInput(req.Target.BaseURL) == "" { // swobu:io-string source=boundary
		return ports.ProviderResponse{}, canonical.BadEndpoint("bedrock provider base URL is required")
	}

	wireReq, err := e.encodeRequest(req.Target, req.Request, req.Contract.ProviderCallMode.Streaming())
	if err != nil {
		return ports.ProviderResponse{}, err
	}
	requestURL := httpedge.JoinBaseURLAndPath(req.Target.BaseURL, wireReq.Path)

	var payload []byte
	if wireReq.Body != nil {
		payload, err = io.ReadAll(wireReq.Body)
		if err != nil {
			return ports.ProviderResponse{}, canonical.InternalError("provider request payload could not be read for signing")
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, wireReq.Method, requestURL, bytes.NewReader(payload))
	if err != nil {
		return ports.ProviderResponse{}, canonical.BadEndpoint("bedrock provider request could not be built")
	}
	if wireReq.HasBody {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)

	if err := applyBedrockAuth(ctx, req.Target.CredentialRef, httpReq, payload); err != nil {
		return ports.ProviderResponse{}, err
	}

	resp, err := e.client.Do(httpReq)
	if err != nil {
		return ports.ProviderResponse{}, canonical.BadEndpoint("bedrock provider request failed before backend response")
	}
	resp, err = httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return ports.ProviderResponse{}, canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		return ports.ProviderResponse{}, httpedge.ReadBackendHTTPError(resp, req.Target.BackendRef)
	}

	if req.Contract.ProviderCallMode.Streaming() {
		return e.decodeStreamingResponse(req.Target, resp.Body)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return ports.ProviderResponse{}, canonical.InternalError("backend success response could not be read")
	}
	return e.decodeBufferedResponse(req.Target, raw)
}

func (e ProviderExecutorAdapter) ListModels(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	mode, value := parseBedrockAuthMode(target.CredentialRef)
	switch mode {
	case bedrockAuthModeAWSProfile:
		return listBedrockModelsViaSDK(ctx, target, value)
	case bedrockAuthModeAPIKeyEnv:
		return e.listModelsViaBearerHTTP(ctx, target)
	default:
		return nil, canonical.BadEndpoint("bedrock auth mode is unsupported")
	}
}

func (e ProviderExecutorAdapter) ValidateCredentials(ctx context.Context, target ports.RoutableTarget) error {
	mode, value := parseBedrockAuthMode(target.CredentialRef)
	switch mode {
	case bedrockAuthModeAWSProfile:
		_, err := listBedrockModelsViaSDK(ctx, target, value)
		return err
	case bedrockAuthModeAPIKeyEnv:
		return e.validateCredentialsViaBearerHTTP(ctx, target)
	default:
		return canonical.BadEndpoint("bedrock auth mode is unsupported")
	}
}

func (e ProviderExecutorAdapter) listModelsViaBearerHTTP(ctx context.Context, target ports.RoutableTarget) ([]string, error) {
	if trimBedrockInput(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("bedrock provider base URL is required")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, httpedge.JoinBaseURLAndPath(target.BaseURL, "/models"), nil)
	if err != nil {
		return nil, canonical.BadEndpoint("bedrock provider model catalog request could not be built")
	}
	httpReq.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
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
		return nil, httpedge.ReadBackendHTTPError(resp, target.BackendRef)
	}
	models, err := modelcatalogopenaicompat.DecodeModelIDs(resp.Body)
	if err != nil {
		return nil, canonical.InternalError("backend model catalog could not be decoded")
	}
	return models, nil
}

func (e ProviderExecutorAdapter) validateCredentialsViaBearerHTTP(ctx context.Context, target ports.RoutableTarget) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodHead, httpedge.JoinBaseURLAndPath(target.BaseURL, "/models"), nil)
	if err != nil {
		return canonical.BadEndpoint("bedrock provider credential validation request could not be built")
	}
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := applyBedrockAuth(ctx, target.CredentialRef, httpReq, nil); err != nil {
		return err
	}
	resp, err := e.client.Do(httpReq)
	if err != nil {
		return canonical.BadEndpoint("bedrock provider credential validation failed before backend response")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return httpedge.ReadBackendHTTPError(resp, target.BackendRef)
	}
	return nil
}

func (e ProviderExecutorAdapter) encodeRequest(target ports.RoutableTarget, req canonical.CanonicalRequest, deliveryMode bool) (protocols.WireRequest, error) {
	dispatch, err := protocolCodecDispatchFor(target.ProviderID(), target.ProtocolKind)
	if err != nil {
		return protocols.WireRequest{}, err
	}
	return dispatch.realize(req, deliveryMode)
}

func (e ProviderExecutorAdapter) decodeBufferedResponse(target ports.RoutableTarget, raw []byte) (ports.ProviderResponse, error) {
	dispatch, err := protocolCodecDispatchFor(target.ProviderID(), target.ProtocolKind)
	if err != nil {
		return ports.ProviderResponse{}, err
	}
	if dispatch.decodeBuffered == nil {
		return ports.ProviderResponse{}, canonical.UnsupportedDelivery("bedrock provider buffered delivery is not implemented")
	}
	return dispatch.decodeBuffered(raw)
}

func (e ProviderExecutorAdapter) decodeStreamingResponse(target ports.RoutableTarget, body io.ReadCloser) (ports.ProviderResponse, error) {
	dispatch, err := protocolCodecDispatchFor(target.ProviderID(), target.ProtocolKind)
	if err != nil {
		_ = body.Close()
		return ports.ProviderResponse{}, err
	}
	if dispatch.decodeStream == nil {
		_ = body.Close()
		return ports.ProviderResponse{}, canonical.UnsupportedDelivery("bedrock provider streaming delivery is not implemented")
	}
	return dispatch.decodeStream(body)
}

func protocolCodecDispatchFor(providerIDRaw string, kind protocolkind.ProtocolKind) (protocolCodecDispatchSpec, error) {
	providerID, ok := providercatalog.ParseProviderID(trimBedrockInput(providerIDRaw)) // swobu:io-string source=boundary
	if !ok || providerID != providercatalog.ProviderSpecBedrock {
		return protocolCodecDispatchSpec{}, canonical.BadEndpoint("provider id is unsupported for bedrock adapter runtime")
	}
	dispatch, ok := protocolDispatchTable[kind]
	if !ok {
		if kind == protocolkind.Messages {
			return protocolCodecDispatchSpec{}, canonical.UnsupportedOperation("bedrock provider does not implement the messages protocol")
		}
		return protocolCodecDispatchSpec{}, canonical.UnsupportedOperation("bedrock provider protocol kind is not implemented")
	}
	return dispatch, nil
}
