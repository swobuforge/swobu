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
// target. Client delivery is transient request input; provider delivery is the
// durable concrete protocol's upstream carrier. Exchange performs only the
// necessary provider/client conversion after this selection.
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
	return providerPath{target: routable, delivery: routable.ProviderDelivery}, nil
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
		Store:            request.StoreField(),
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
	normalizedProviderProtocol, err := profile.NormalizeProviderProtocolForSpec(providerSpec, providerProtocol)
	if err != nil {
		return provider.TargetSnapshot{}, canonical.BadEndpoint(err.Error())
	}
	protocolSpec, ok := profile.ProviderProtocolSpecForSpec(providerSpec, normalizedProviderProtocol)
	if !ok {
		return provider.TargetSnapshot{}, canonical.BadEndpoint("selected provider protocol is unsupported")
	}
	protocolKind := protocolSpec.Kind
	providerDelivery := protocolSpec.Delivery
	switch connection := connection.(type) {
	case routing.StandardConnection:
		baseURL := profile.DefaultExecuteBaseURL(providerSpec)
		if configured, ok := connection.Locator(); ok {
			baseURL = configured.String()
		}
		return provider.NewTargetSnapshot(targetID, providerSpec, baseURL, connection.Credential().String(), protocolKind, normalizedProviderProtocol, providerDelivery), nil
	case routing.ZAIConnection:
		return provider.NewTargetSnapshot(targetID, providerSpec, connection.BaseURL(), connection.Credential().String(), protocolKind, normalizedProviderProtocol, providerDelivery), nil
	case routing.BedrockConnection:
		region := connection.Region().String()
		resolution, err := profile.ResolveBedrockEndpoint(connection.Endpoint(), region, protocolKind)
		if err != nil {
			return provider.TargetSnapshot{}, canonical.BadEndpoint(err.Error())
		}
		return provider.NewBedrockTargetSnapshot(targetID, resolution.BaseURL, connection.Credential().String(), protocolKind, normalizedProviderProtocol, region, providerDelivery), nil
	case routing.CustomConnection:
		baseURL := connection.BaseURL().String()
		credential := ""
		authHeader := ""
		if connection.Auth() != nil {
			header := connection.Auth().(routing.CustomHeaderAuth)
			credential = header.Credential().String()
			authHeader = header.Name()
		}
		return provider.NewCustomTargetSnapshot(targetID, baseURL, credential, protocolKind, normalizedProviderProtocol, authHeader, providerDelivery), nil
	default:
		return provider.TargetSnapshot{}, fmt.Errorf("unsupported routing connection %T", connection)
	}
}
