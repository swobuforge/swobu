package exchange

import (
	"context"
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/session"
)

// runtimeBundle contains the explicit dependencies used by exchange commands.
type runtimeBundle struct {
	Runtime         ExecutionRuntime
	TrafficEvidence observation.TrafficEventSink
	CheckpointStore session.Store
	ResponseIDs     ResponseIDGenerator
	Policy          WorkspacePolicy
	ImageFetcher    provider.ImageFetcher
	PolicyResolver  WorkspacePolicyResolver
}

func allocateResponseID(ctx context.Context, exchangeID string, gen ResponseIDGenerator) (canonical.SwobuResponseID, error) {
	if gen == nil {
		return "", errors.New("exchange response id generator is required")
	}
	responseID, err := gen.NewSwobuResponseID(ctx, exchangeID)
	if err != nil {
		return "", err
	}
	return responseID, nil
}

func validateCheckpointRuntime(r runtimeBundle) error {
	if r.CheckpointStore == nil {
		return errors.New("exchange checkpoint store is required")
	}
	if r.ResponseIDs == nil {
		return errors.New("exchange response id generator is required")
	}
	return nil
}

func validateCheckpointInput(r runtimeBundle, workspaceSlug string) error {
	if err := validateCheckpointRuntime(r); err != nil {
		return err
	}
	if strings.TrimSpace(workspaceSlug) == "" { // swobu:io-string source=boundary
		return errors.New("exchange checkpoint workspace slug is required")
	}
	return nil
}

// ---- helpers used by provider-round execution ----

func encodeClientOutput(ctx context.Context, call providerCall, envelope canonical.ResponseStream, incremental bool) (ClientResponse, error) {
	if call.clientDelivery.Mode == delivery.Streaming {
		if call.clientDelivery.Framing == delivery.FramingWebSocket {
			messageResult, err := call.clientCodec.EncodeResponseMessages(ctx, call.fullRequest, envelope, call.clientDelivery)
			if err != nil {
				return nil, err
			}
			return NewMessageStreamingResponse(messageResult.Response, messageResult.Completion), nil
		}
		streamResult, err := call.clientCodec.EncodeResponseStream(ctx, call.fullRequest, envelope, call.clientDelivery)
		if err != nil {
			return nil, err
		}
		return NewStreamingResponse(streamResult.Stream, streamResult.Completion), nil
	}
	return newBufferedClientResponse(newBufferedClientBody(ctx, call, envelope)), nil
}
