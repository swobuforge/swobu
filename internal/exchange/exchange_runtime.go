package exchange

import (
	"context"
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/continuity"
	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/mcp"
	"github.com/swobuforge/swobu/internal/observation"
	"github.com/swobuforge/swobu/internal/provider"
	"github.com/swobuforge/swobu/internal/routing"
)

// runtimeBundle contains the explicit dependencies used by exchange commands.
type runtimeBundle struct {
	ModelConnection func(connectionidentity.ID) (connections.Connection, error)
	// PublishRefreshedCredential stages immutable material and compares the
	// captured Connection revision before publishing a replacement binding.
	PublishRefreshedCredential func(context.Context, provider.TargetSnapshot, string) error

	localToolDelivery      *localToolDelivery
	Accounting             *accounting.Store
	PriceEstimator         PriceEstimator
	Runtime                ExecutionRuntime
	ExecutionEvidence      observation.ExecutionEvidenceSink
	CheckpointStore        continuity.Store
	ResponseIDs            ResponseIDGenerator
	Policy                 WorkspacePolicy
	ImageFetcher           provider.ImageFetcher
	PolicyResolver         WorkspacePolicyResolver
	TargetBackoff          *targetBackoffLedger
	TargetExceptions       *targetExceptions
	WorkspaceToolCatalog   func(context.Context, routing.Workspace) ([]mcp.CatalogTool, error)
	WorkspaceToolCall      func(context.Context, accessidentity.ID, routing.Workspace, mcp.CatalogTool, canonical.ToolCallItem) (canonical.CanonicalItem, error)
	selectedWorkspaceTools []mcp.CatalogTool
	workspaceToolExecute   func(context.Context, mcp.CatalogTool, canonical.ToolCallItem) (canonical.CanonicalItem, error)
}

// PriceEstimator supplies a target-pinned quote, including economic identity
// and quantity coverage. A false result remains unknown, never a zero price.
type PriceEstimator interface {
	Estimate(provider.TargetSnapshot, canonical.CanonicalRequest) (accounting.Quote, bool)
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
