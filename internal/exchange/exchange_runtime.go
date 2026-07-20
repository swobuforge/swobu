package exchange

import (
	"context"
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/replay"
)

// runtimeBundle contains the explicit dependencies used by exchange commands.
type runtimeBundle struct {
	Runtime          ExecutionRuntime
	DecisionSink     compat.Sink
	ReplayStore      replay.Store
	SwobuResponseIDs replay.SwobuResponseIDGenerator
	Policy           WorkspacePolicy
	ImageFetcher     provider.ImageFetcher
	PolicyResolver   WorkspacePolicyResolver
}

func allocateSwobuResponseID(ctx context.Context, exchangeID string, gen replay.SwobuResponseIDGenerator) (canonical.SwobuResponseID, error) {
	if gen == nil {
		return "", errors.New("exchange response id generator is required")
	}
	responseID, err := gen.NewSwobuResponseID(ctx, exchangeID)
	if err != nil {
		return "", err
	}
	return responseID, nil
}

func validateReplayRuntime(r runtimeBundle) error {
	if r.ReplayStore == nil {
		return errors.New("exchange replay store is required")
	}
	if r.SwobuResponseIDs == nil {
		return errors.New("exchange response id generator is required")
	}
	return nil
}

func validateReplayInput(r runtimeBundle, workspaceSlug string) error {
	if err := validateReplayRuntime(r); err != nil {
		return err
	}
	if strings.TrimSpace(workspaceSlug) == "" { // swobu:io-string source=boundary
		return errors.New("exchange replay workspace slug is required")
	}
	return nil
}

// ---- helpers used by provider-round execution ----

func encodeClientOutput(ctx context.Context, call providerCall, envelope canonical.ResponseStream, incremental bool, sink compat.Sink) (ClientResponse, error) {
	commitDecisionsBestEffort(ctx, sink, call.exchangeID, deliveryCompatibilityDecisions(call, incremental))

	if call.clientDelivery.Mode == delivery.Streaming {
		if call.clientDelivery.Framing == delivery.FramingWebSocket {
			messageResult, err := call.clientCodec.EncodeResponseMessages(ctx, envelope, call.clientDelivery)
			commitDecisionsBestEffort(ctx, sink, call.exchangeID, messageResult.Decisions)
			if err != nil {
				return nil, err
			}
			return NewMessageStreamingResponse(messageResult.Response), nil
		}
		streamResult, err := call.clientCodec.EncodeResponseStream(ctx, envelope, call.clientDelivery)
		commitDecisionsBestEffort(ctx, sink, call.exchangeID, streamResult.Decisions)
		if err != nil {
			return nil, err
		}
		return NewStreamingResponse(streamResult.Stream), nil
	}
	return newBufferedClientResponse(newBufferedClientBody(ctx, call, envelope, sink)), nil
}
