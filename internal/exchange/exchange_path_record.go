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
		Items:            request.Items(),
		PreviousResponse: previousResponse,
		ToolPolicy:       request.ToolPolicyField(),
		ToolCallBatch:    request.ToolCallBatchField(),
		Controls:         request.Controls(),
		Reasoning:        request.Reasoning(),
		OutputFormat:     request.OutputFormatField(),
		Responses:        request.Responses(),
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
// routing connection at the single exchange boundary. Each provider arm
// constructs its snapshot with the provider-specific facts fixed at construction
// (the Bedrock signing region via NewBedrockTargetSnapshot, the custom auth
// header via NewCustomTargetSnapshot) — no incomplete snapshot is completed by
// post-construction mutation. Derived URLs never enter persistence.
func ProviderTargetFromConnection(targetID string, connection routing.Connection, providerProtocol string) (provider.TargetSnapshot, error) {
	providerSpec := string(connection.Provider())
	protocolKind, frame, ok := profile.ProviderProtocolKindAndFrame(providerSpec, providerProtocol)
	if !ok {
		return provider.TargetSnapshot{}, canonical.BadEndpoint("selected provider protocol is unsupported")
	}
	switch connection := connection.(type) {
	case routing.APIKeyConnection:
		return provider.NewTargetSnapshot(targetID, providerSpec, profile.DefaultExecuteBaseURL(providerSpec), connection.Credential().String(), protocolKind, frame, providerProtocol), nil
	case routing.ZAIConnection:
		return provider.NewTargetSnapshot(targetID, providerSpec, connection.BaseURL(), connection.Credential().String(), protocolKind, frame, providerProtocol), nil
	case routing.OllamaConnection:
		baseURL := profile.DefaultExecuteBaseURL(providerSpec)
		if configured, ok := connection.BaseURL(); ok {
			baseURL = configured.String()
		}
		return provider.NewTargetSnapshot(targetID, providerSpec, baseURL, "", protocolKind, frame, providerProtocol), nil
	case routing.AzureConnection:
		return provider.NewTargetSnapshot(targetID, providerSpec, connection.ProjectEndpoint().String(), connection.Credential().String(), protocolKind, frame, providerProtocol), nil
	case routing.BedrockConnection:
		region := connection.Region().String()
		resolution, err := profile.ResolveBedrockEndpoint(connection.Endpoint(), region, protocolKind)
		if err != nil {
			return provider.TargetSnapshot{}, canonical.BadEndpoint(err.Error())
		}
		return provider.NewBedrockTargetSnapshot(targetID, resolution.BaseURL, connection.Credential().String(), protocolKind, frame, providerProtocol, region), nil
	case routing.CustomConnection:
		baseURL := connection.BaseURL().String()
		credential := ""
		authHeader := ""
		if connection.Auth() != nil {
			header := connection.Auth().(routing.CustomHeaderAuth)
			credential = header.Credential().String()
			authHeader = header.Name()
		}
		return provider.NewCustomTargetSnapshot(targetID, baseURL, credential, protocolKind, frame, providerProtocol, authHeader), nil
	default:
		return provider.TargetSnapshot{}, fmt.Errorf("unsupported routing connection %T", connection)
	}
}
