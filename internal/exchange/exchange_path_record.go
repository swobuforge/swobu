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
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

type executionFailureKind uint8

const (
	failureRequestInvalid executionFailureKind = iota
	failureUnsupportedByBackend
	failureBackendUnavailable
	failureBackendRejected
	failureCancelled
	failureInternal
)

func classifyExecutionFailure(err error) executionFailureKind {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return failureCancelled
	}
	var unsupported provider.UnsupportedError
	if errors.As(err, &unsupported) {
		return failureUnsupportedByBackend
	}
	var unavailable provider.UnavailableError
	if errors.As(err, &unavailable) {
		return failureBackendUnavailable
	}
	var rejected provider.RejectedError
	if errors.As(err, &rejected) {
		return failureBackendRejected
	}
	var invalid provider.InvalidRequestError
	if errors.As(err, &invalid) {
		return failureRequestInvalid
	}
	var cancelled provider.CancelledError
	if errors.As(err, &cancelled) {
		return failureCancelled
	}
	var internal provider.InternalError
	if errors.As(err, &internal) {
		return failureInternal
	}
	var canonicalErr canonical.Error
	if errors.As(err, &canonicalErr) {
		switch canonicalErr.Code {
		case canonical.ErrorCodeBadRequest, canonical.ErrorCodeUnsupportedOperation,
			canonical.ErrorCodeUnsupportedDelivery, canonical.ErrorCodeUnsupportedEndpoint,
			canonical.ErrorCodeBadEndpoint, canonical.ErrorCodeUnknownTarget:
			return failureRequestInvalid
		default:
			return failureInternal
		}
	}
	return failureInternal
}

func fallbackEligibleFailure(err error) bool {
	switch classifyExecutionFailure(err) {
	case failureUnsupportedByBackend, failureBackendUnavailable:
		return true
	default:
		return false
	}
}

func buildPathRecord(target routing.Target, request canonical.CanonicalRequest) (exchangePathRecord, error) {
	routable, err := toProviderTarget(target)
	if err != nil {
		return exchangePathRecord{}, err
	}
	protocol, frame, ok := profile.ProviderProtocolKindAndFrame(routable.ProviderSpec, target.Protocol().String())
	if !ok {
		return exchangePathRecord{}, canonical.BadEndpoint("selected provider protocol is unsupported")
	}
	modelID := target.Model().String()
	if modelID == "" {
		return exchangePathRecord{}, canonical.BadRequest("selected provider model is not configured")
	}
	routable.Model = modelID
	req := canonical.NewCanonicalRequest(canonical.RequestParams{Model: modelID, Instructions: request.Instructions(), Items: request.Items(), Tools: request.Tools(), Turn: request.Turn(), ToolPolicy: request.ToolPolicy(), ToolCallBatch: request.ToolCallBatch(), Controls: request.Controls(), OutputFormat: request.OutputFormat(), Presence: request.Presence()})
	providerDelivery := delivery.BufferedDelivery()
	if frame == profile.FrameSSEEvent {
		providerDelivery = delivery.StreamingDelivery(delivery.FramingSSE)
	}
	return exchangePathRecord{Request: req, Target: routable, ProviderDelivery: providerDelivery, ProtocolKind: protocol}, nil
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

func resolveProviderProtocolKind(targetSpec, targetProtocol string, configured protocolkind.ProtocolKind, request canonical.CanonicalRequest) (protocolkind.ProtocolKind, error) {
	if !profile.SupportsSpec(targetSpec) {
		return "", canonical.BadEndpoint("provider id is unsupported")
	}
	if strings.TrimSpace(request.Model()) == "" { // swobu:io-string source=domain
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
	Target           provider.TargetSnapshot
	ProviderDelivery delivery.Delivery
	ProtocolKind     protocolkind.ProtocolKind
}
