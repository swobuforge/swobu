package exchange

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

// providerPath is the single resolved execution projection for one route
// target. Provider delivery is derived from the selected protocol frame.
type providerPath struct {
	target   provider.TargetSnapshot
	delivery delivery.Delivery
}

func resolveProviderPath(target routing.Target) (providerPath, error) {
	routable, err := toProviderTarget(target)
	if err != nil {
		return providerPath{}, err
	}
	modelID := target.Model().String()
	if modelID == "" {
		return providerPath{}, canonical.BadRequest("selected provider model is not configured")
	}
	routable.Model = modelID
	if routable.ProviderProtocol != target.Protocol().String() {
		return providerPath{}, canonical.BadEndpoint("provider protocol projection is incoherent")
	}
	if err := routable.ValidateExecutionProtocol(); err != nil {
		return providerPath{}, canonical.BadEndpoint(err.Error())
	}
	providerDelivery := delivery.BufferedDelivery()
	if routable.SelectedFrame == profile.FrameSSEEvent {
		providerDelivery = delivery.StreamingDelivery(delivery.FramingSSE)
	}
	return providerPath{target: routable, delivery: providerDelivery}, nil
}

func bindRequestToTarget(request canonical.CanonicalRequest, modelID string) canonical.CanonicalRequest {
	var previousResponse *canonical.ResponseRef
	if previous, ok := request.PreviousResponse(); ok {
		previousResponse = &previous
	}
	return canonical.NewCanonicalRequest(canonical.RequestParams{
		Model:            canonical.Specify(modelID),
		Instructions:     request.InstructionsField(),
		Items:            request.Items(),
		Tools:            request.ToolsField(),
		PreviousResponse: previousResponse,
		ToolPolicy:       request.ToolPolicyField(),
		ToolCallBatch:    request.ToolCallBatchField(),
		Controls:         request.Controls(),
		OutputFormat:     request.OutputFormatField(),
	})
}

// toProviderTarget is the single adapter from durable connection intent to
// provider execution data. Derived URLs and auth modes never enter persistence.
func toProviderTarget(target routing.Target) (provider.TargetSnapshot, error) {
	snapshot, err := ProviderTargetFromConnection(target.ID().String(), target.Connection(), target.Protocol().String())
	if err != nil {
		return provider.TargetSnapshot{}, err
	}
	snapshot.TargetVersion = uint64(target.Version())
	return snapshot, nil
}

// ProviderTargetFromConnection derives provider execution data from the typed
// routing connection at the single exchange boundary.
func ProviderTargetFromConnection(targetID string, connection routing.Connection, providerProtocol string) (provider.TargetSnapshot, error) {
	providerSpec := string(connection.Provider())
	baseURL := profile.DefaultExecuteBaseURL(providerSpec)
	credential := ""
	authHeader := ""
	switch connection := connection.(type) {
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
		credential = connection.Credential().String()
	case routing.CustomConnection:
		baseURL = connection.BaseURL().String()
		if connection.Auth() != nil {
			header := connection.Auth().(routing.CustomHeaderAuth)
			credential = header.Credential().String()
			authHeader = header.Name()
		}
	default:
		return provider.TargetSnapshot{}, fmt.Errorf("unsupported routing connection %T", connection)
	}
	protocolKind, frame, ok := profile.ProviderProtocolKindAndFrame(providerSpec, providerProtocol)
	if !ok {
		return provider.TargetSnapshot{}, canonical.BadEndpoint("selected provider protocol is unsupported")
	}
	routable := provider.NewTargetSnapshot(targetID, providerSpec, baseURL, credential, protocolKind, frame, providerProtocol)
	routable.AuthHeader = authHeader
	return routable, nil
}
