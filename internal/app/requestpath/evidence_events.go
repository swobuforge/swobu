package requestpath

import (
	"context"
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/runtimeevidence"
	"github.com/swobuforge/swobu/internal/ports"
)

func emitEvidenceEventIfValid(ctx context.Context, sink ports.RequestEvidenceSink, event runtimeevidence.TrafficEvent, err error) {
	if err != nil {
		return
	}
	sink.Append(ctx, event)
}

func newInflightEvidenceEvent(
	requestID string,
	endpointName endpointintent.EndpointName,
	target ports.RoutableTarget,
	provenance IngressProvenance,
	requestedModel string,
	resolvedModel string,
	resolutionMode string,
) (runtimeevidence.TrafficEvent, error) {
	id, err := runtimeevidence.ParseRequestID(requestID)
	if err != nil {
		return runtimeevidence.TrafficEvent{}, err
	}
	route, err := runtimeevidence.NewRoute(target.BackendRef, "")
	if err != nil {
		return runtimeevidence.TrafficEvent{}, err
	}
	return runtimeevidence.NewInflightTrafficEvent(runtimeevidence.TrafficEventInput{
		RequestID:           id,
		Endpoint:            endpointName.String(),
		ClientProtocol:      runtimeevidence.ClientProtocol(strings.TrimSpace(provenance.ClientProtocol)),       // trimlowerlint:allow boundary canonicalization
		IngressFamily:       runtimeevidence.IngressFamily(strings.TrimSpace(string(provenance.IngressFamily))), // trimlowerlint:allow boundary canonicalization
		NormalizedOp:        runtimeevidence.NormalizedOp(strings.TrimSpace(string(provenance.NormalizedOp))),   // trimlowerlint:allow boundary canonicalization
		ClientHandler:       runtimeevidence.ClientHandler(strings.TrimSpace(provenance.ClientHandler)),         // trimlowerlint:allow boundary canonicalization
		Route:               route,
		AttemptCount:        1,
		ModelRequested:      requestedModel,
		ModelResolved:       resolvedModel,
		ModelResolutionMode: resolutionMode,
	})
}

func newSuccessEvidenceEvent(
	requestID string,
	endpointName endpointintent.EndpointName,
	target ports.RoutableTarget,
	provenance IngressProvenance,
	attemptCount int,
	continuityRecovered bool,
	continuityRecoveryTrigger string,
	requestedModel string,
	resolvedModel string,
	resolutionMode string,
	timing runtimeevidence.Timing,
	tokenUsage runtimeevidence.TokenUsage,
) (runtimeevidence.TrafficEvent, error) {
	id, err := runtimeevidence.ParseRequestID(requestID)
	if err != nil {
		return runtimeevidence.TrafficEvent{}, err
	}
	route, err := runtimeevidence.NewRoute(target.BackendRef, "")
	if err != nil {
		return runtimeevidence.TrafficEvent{}, err
	}
	return runtimeevidence.NewTerminalTrafficEvent(runtimeevidence.TrafficEventInput{
		RequestID:                 id,
		Endpoint:                  endpointName.String(),
		ClientProtocol:            runtimeevidence.ClientProtocol(strings.TrimSpace(provenance.ClientProtocol)),       // trimlowerlint:allow boundary canonicalization
		IngressFamily:             runtimeevidence.IngressFamily(strings.TrimSpace(string(provenance.IngressFamily))), // trimlowerlint:allow boundary canonicalization
		NormalizedOp:              runtimeevidence.NormalizedOp(strings.TrimSpace(string(provenance.NormalizedOp))),   // trimlowerlint:allow boundary canonicalization
		ClientHandler:             runtimeevidence.ClientHandler(strings.TrimSpace(provenance.ClientHandler)),         // trimlowerlint:allow boundary canonicalization
		Route:                     route,
		Result:                    runtimeevidence.ResultClassSuccess,
		StatusCode:                200,
		AttemptCount:              attemptCount,
		ContinuityRecovered:       continuityRecovered,
		ContinuityRecoveryTrigger: continuityRecoveryTrigger,
		ModelRequested:            requestedModel,
		ModelResolved:             resolvedModel,
		ModelResolutionMode:       resolutionMode,
		Timing:                    timing,
		TokenUsage:                tokenUsage,
	})
}

func newErrorEvidenceEvent(
	requestID string,
	endpointName endpointintent.EndpointName,
	target ports.RoutableTarget,
	provenance IngressProvenance,
	err error,
	requestedModel string,
	resolvedModel string,
	resolutionMode string,
) (runtimeevidence.TrafficEvent, error) {
	id, parseErr := runtimeevidence.ParseRequestID(requestID)
	if parseErr != nil {
		return runtimeevidence.TrafficEvent{}, parseErr
	}
	route, routeErr := runtimeevidence.NewRoute(target.BackendRef, "")
	if routeErr != nil {
		return runtimeevidence.TrafficEvent{}, routeErr
	}
	input := runtimeevidence.TrafficEventInput{
		RequestID:           id,
		Endpoint:            endpointName.String(),
		ClientProtocol:      runtimeevidence.ClientProtocol(strings.TrimSpace(provenance.ClientProtocol)),       // trimlowerlint:allow boundary canonicalization
		IngressFamily:       runtimeevidence.IngressFamily(strings.TrimSpace(string(provenance.IngressFamily))), // trimlowerlint:allow boundary canonicalization
		NormalizedOp:        runtimeevidence.NormalizedOp(strings.TrimSpace(string(provenance.NormalizedOp))),   // trimlowerlint:allow boundary canonicalization
		ClientHandler:       runtimeevidence.ClientHandler(strings.TrimSpace(provenance.ClientHandler)),         // trimlowerlint:allow boundary canonicalization
		Route:               route,
		Result:              runtimeevidence.ResultClassSwobuError,
		AttemptCount:        1,
		ModelRequested:      requestedModel,
		ModelResolved:       resolvedModel,
		ModelResolutionMode: resolutionMode,
	}

	var backendErr canonical.BackendError
	if errors.As(err, &backendErr) {
		input.Result = runtimeevidence.ResultClassBackendError
		input.StatusCode = backendErr.StatusCode
	} else {
		var canonicalErr canonical.Error
		if errors.As(err, &canonicalErr) {
			input.Result = resultClassForSwobuError(canonicalErr.Code)
		}
	}
	return runtimeevidence.NewTerminalTrafficEvent(input)
}

func resultClassForSwobuError(code canonical.ErrorCode) runtimeevidence.ResultClass {
	switch code {
	case canonical.ErrorCodeUnsupportedOperation:
		return runtimeevidence.ResultClassUnsupportedOperation
	case canonical.ErrorCodeUnsupportedDelivery:
		return runtimeevidence.ResultClassUnsupportedDeliveryVariant
	default:
		return runtimeevidence.ResultClassSwobuError
	}
}

func tokenUsageFromExecuteResponse(response ports.ProviderResponse) runtimeevidence.TokenUsage {
	_ = response
	return runtimeevidence.NewUnknownTokenUsage()
}
