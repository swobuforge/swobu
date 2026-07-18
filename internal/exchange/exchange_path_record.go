package exchange

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

func mapErrorToFailureClass(err error) routing.FailureClass {
	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		switch status := backendErr.StatusCode; {
		case status == 404:
			return routing.FailureNotFound
		case status == 400:
			return routing.FailureBadRequest
		case status == 401 || status == 403:
			return routing.FailureAuth
		case status == 429:
			return routing.FailureRateLimited
		case status >= 500 && status < 600:
			return routing.FailureServerError
		default:
			return routing.FailureNetwork
		}
	}
	return routing.FailureUnknown
}

func buildPathRecord(ctx context.Context, exchangeID string, target routing.Target, clientDelivery delivery.Delivery, request canonical.CanonicalRequest) (exchangePathRecord, error) {
	_ = ctx
	_ = exchangeID
	routable, err := toRoutableTarget(target)
	if err != nil {
		return exchangePathRecord{}, err
	}
	protocol, _, ok := profile.ProviderProtocolKindAndFrame(routable.ProviderSpec, target.Protocol().String())
	if !ok {
		return exchangePathRecord{}, canonical.BadEndpoint("selected provider protocol is unsupported")
	}
	modelID := target.Model().String()
	if modelID == "" {
		return exchangePathRecord{}, canonical.BadRequest("selected provider model is not configured")
	}
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: modelID, Instructions: request.Instructions(), Items: request.Items(), Tools: request.Tools(), Turn: request.Turn(), ToolPolicy: request.ToolPolicy(), ToolCallBatch: request.ToolCallBatch(), Controls: request.Controls(), OutputFormat: request.OutputFormat()})
	return exchangePathRecord{Request: req, Target: routable, ProviderDelivery: clientDelivery, ProtocolKind: protocol}, nil
}

// toRoutableTarget is the single adapter from durable connection intent to
// provider execution data. Derived URLs and auth modes never enter persistence.
func toRoutableTarget(target routing.Target) (RoutableTarget, error) {
	providerID := string(target.Provider())
	providerSpec := providerID
	if target.Provider() == routing.ProviderCustom {
		providerSpec = "openai_compatible"
	}
	baseURL := profile.DefaultExecuteBaseURL(providerSpec)
	credential := ""
	authMode := ""
	authHeader := ""
	switch connection := target.Connection().(type) {
	case routing.OpenAIConnection:
		credential = connection.Credential().String()
	case routing.AnthropicConnection:
		credential = connection.Credential().String()
	case routing.OpenRouterConnection:
		credential = connection.Credential().String()
	case routing.ChatGPTConnection:
		credential = connection.Credential().String()
	case routing.OllamaConnection:
		if configured, ok := connection.BaseURL(); ok {
			baseURL = configured.String()
		}
	case routing.AzureConnection:
		baseURL = connection.ProjectEndpoint().String()
		credential = connection.Credential().String()
	case routing.BedrockConnection:
		baseURL = profile.BedrockMantleEndpointForRegion(connection.Region().String())
		switch auth := connection.Auth().(type) {
		case routing.BedrockProfileAuth:
			authMode = string(profile.AuthModeAWSProfile)
			credential = "profile:" + auth.Profile() + "@" + connection.Region().String()
		case routing.BedrockEnvironmentAuth:
			authMode = string(profile.AuthModeAWSEnvSession)
		case routing.BedrockBearerTokenAuth:
			authMode = string(profile.AuthModeEnv)
			credential = auth.Credential().String()
		default:
			return RoutableTarget{}, fmt.Errorf("unsupported Bedrock auth %T", connection.Auth())
		}
	case routing.CustomConnection:
		baseURL = connection.BaseURL().String()
		if connection.Auth() != nil {
			header := connection.Auth().(routing.CustomHeaderAuth)
			credential = header.Credential().String()
			authHeader = header.Name()
		}
	default:
		return RoutableTarget{}, fmt.Errorf("unsupported routing connection %T", target.Connection())
	}
	protocolKind, frame, ok := profile.ProviderProtocolKindAndFrame(providerSpec, target.Protocol().String())
	if !ok {
		return RoutableTarget{}, canonical.BadEndpoint("selected provider protocol is unsupported")
	}
	routable := NewRoutableTarget(target.ID().String(), providerSpec, baseURL, credential, protocolKind, "", frame, target.Protocol().String())
	routable.AuthMode = authMode
	routable.AuthHeader = authHeader
	return routable, nil
}

func resolveProviderProtocolKind(targetSpec, targetProtocol string, configured protocolkind.ProtocolKind, request canonical.CanonicalRequest) (protocolkind.ProtocolKind, error) {
	if !profile.SupportsSpec(targetSpec) {
		return "", canonical.BadEndpoint("provider id is unsupported")
	}
	if strings.TrimSpace(request.Model()) == "" {
		return "", canonical.BadRequest("canonical request is required")
	}
	if targetProtocol == "" || targetProtocol == profile.ProviderProtocolAuto {
		return "", canonical.BadEndpoint("provider protocol must be concrete")
	}
	protocol, _, ok := profile.ProviderProtocolKindAndFrame(targetSpec, targetProtocol)
	if !ok || (configured != "" && protocol != configured) {
		return "", canonical.BadEndpoint("selected provider protocol is unsupported")
	}
	return protocol, nil
}

type exchangePathRecord struct {
	Request          canonical.CanonicalRequest
	Target           RoutableTarget
	ProviderDelivery delivery.Delivery
	ProtocolKind     protocolkind.ProtocolKind
}
