package bedrock

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	modelcatalogopenai "github.com/swobuforge/swobu/internal/adapters/outbound/modelcatalog/openai"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	exchangeruntime "github.com/swobuforge/swobu/internal/adapters/wire/exchangeruntime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/ports"
	"github.com/swobuforge/swobu/internal/profile"
)

const swobuCallerUAHeaderValue = "swobu/dev"

type ProviderIngressResolverAdapter struct {
	client *http.Client
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

	path, err := exchangeruntime.ProviderRequestPath(req.Target.ProviderID(), req.Target.ProtocolKind)
	if err != nil {
		return nil, err
	}

	wireReqCarrier := req.RequestDocument
	if wireReqCarrier.IsEmpty() {
		return nil, canonical.InternalError("provider request document is required")
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

	if err := applyBedrockAuth(ctx, req.Target.CredentialRef, httpReq, wireReqBody); err != nil {
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
		return carrier.WireStream{
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
	return carrier.NewWireDocument(
		carrier.StageProviderIngressIn,
		req.Target.ProtocolKind,
		"application/json",
		resp.Header.Clone(),
		raw,
		carrier.Meta{},
	), nil
}

func (e ProviderIngressResolverAdapter) ListModels(ctx context.Context, target exchange.RoutableTarget) ([]string, error) {
	if strings.TrimSpace(target.BaseURL) == "" { // swobu:io-string source=boundary
		return nil, canonical.BadEndpoint("bedrock provider base URL is required")
	}
	if err := validateBedrockMantleEndpoint(target.BaseURL); err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, httpedge.JoinBaseURLAndPath(target.BaseURL, "/models"), nil)
	if err != nil {
		return nil, canonical.BadEndpoint("bedrock provider model catalog request could not be built")
	}
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := applyBedrockAuth(ctx, target.CredentialRef, httpReq, nil); err != nil {
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
	return models, nil
}

func (e ProviderIngressResolverAdapter) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	_, err := e.ListModels(ctx, target)
	return err
}
