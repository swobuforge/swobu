// compatibility request lifecycle in one application seam.
package requestpath

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/compatibility"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/ports"
)

type RequestHandler struct {
	endpoints         ports.EndpointReader
	providers         ports.ProviderExecutor
	evidence          ports.RequestEvidenceSink
	continuity        ports.ResponseContinuityStore
	capabilityCatalog BackendModelCapabilityCatalog
	routingPolicy     RoutingPolicy
	attemptPipeline   AttemptPipeline
}

// NewRequestHandler wires the application boundary for one request path. Nil
// ports are allowed at construction time so composition can stay simple, but
// Handle rejects them explicitly at runtime instead of guessing fallback behavior.
func NewRequestHandler(endpoints ports.EndpointReader, providers ports.ProviderExecutor, evidence ports.RequestEvidenceSink, continuity ports.ResponseContinuityStore) RequestHandler {
	handler := RequestHandler{
		endpoints:         endpoints,
		providers:         providers,
		evidence:          evidence,
		continuity:        continuity,
		capabilityCatalog: defaultBackendModelCapabilityCatalog(),
		routingPolicy:     staticRoutingPolicy{},
	}
	handler.attemptPipeline = handler.defaultAttemptPipeline()
	return handler
}

// HandleInput carries one semantic request lifecycle into the app layer.
// Request-scoped execution metadata such as RequestID lives here rather than in
// canonical request objects so compatibility types stay semantic-only.
type HandleInput struct {
	EndpointName endpointintent.EndpointName
	RequestID    string
	Request      compatibility.CanonicalRequest
	Contract     ExecutionContract
	Provenance   IngressProvenance
}

// IngressProvenance captures normalized, metadata-only ingress context carried
// from the HTTP edge into evidence truth.
type IngressProvenance struct {
	ClientProtocol string
	IngressFamily  compatibility.IngressFamily
	NormalizedOp   compatibility.NormalizedPath
	ClientHandler  string
}

// HandleOutput returns the selected target plus the semantic provider
// response. The app layer does not rewrite successful semantics; that remains a
// client-edge responsibility in the inbound adapter.
type HandleOutput struct {
	Response ports.ExecuteResponse
	Target   ports.RoutableTarget
}

type ListModelsInput struct {
	EndpointName endpointintent.EndpointName
}

type ModelOption struct {
	ID           string
	ModelID      string
	ProviderSpec string
	BackendRef   string
}

type ListModelsOutput struct {
	DefaultModelID string
	Models         []ModelOption
}

// Handle resolves durable intent, asks compatibility to prepare any
// continuation-aware request views, emits runtime evidence, and delegates
// semantic execution to the provider port. It preserves Swobu-vs-backend error
// origin rather than laundering failures into one generic class.
// resolution, compatibility prep, provider execution, and evidence emission.
// independent failure and delivery paths that belong in one orchestrator.
func (o RequestHandler) Handle(ctx context.Context, in HandleInput) (HandleOutput, error) {
	if in.EndpointName.IsZero() {
		return HandleOutput{}, compatibility.BadEndpoint("endpoint name is required")
	}
	if in.Request == nil {
		return HandleOutput{}, compatibility.BadRequest("canonical request is required")
	}
	if strings.TrimSpace(string(in.Contract.DeliveryMode)) == "" {
		return HandleOutput{}, compatibility.BadRequest("execution contract delivery mode is required")
	}
	if o.endpoints == nil {
		return HandleOutput{}, compatibility.InternalError("endpoint reader is not configured")
	}
	if o.providers == nil {
		return HandleOutput{}, compatibility.InternalError("provider executor is not configured")
	}
	if o.routingPolicy == nil {
		return HandleOutput{}, compatibility.InternalError("routing policy is not configured")
	}

	endpoint, err := o.endpoints.GetEndpoint(ctx, in.EndpointName)
	if err != nil {
		return HandleOutput{}, compatibility.BadEndpoint("endpoint could not be resolved")
	}

	intent := ClientIntent{
		EndpointName:   in.EndpointName,
		RequestID:      in.RequestID,
		Request:        compatibility.CloneCanonicalRequest(in.Request),
		RequestedModel: requestModel(in.Request),
		Provenance:     in.Provenance,
	}
	route, err := o.routingPolicy.Decide(ctx, endpoint, intent)
	if err != nil {
		return HandleOutput{}, err
	}
	resolvedRequest := materializeRequestForExecution(intent.Request, route.EffectiveModel)
	backendModel := BackendModelEntity{
		BackendRef:     route.Target.BackendRef,
		ProviderSpec:   route.Target.ProviderSpecName(),
		ProtocolKind:   route.Target.ProtocolKind,
		BackendModelID: route.EffectiveModel,
	}
	outcome := o.executeAttempt(ctx, ExecutionAttempt{
		Intent:       intent,
		Route:        route,
		Index:        1,
		Contract:     in.Contract,
		Request:      resolvedRequest,
		Capabilities: o.capabilityCatalog.SnapshotFor(backendModel),
		Continuation: compatibility.NewContinuationRuntime(o.continuity),
	})
	if outcome.Err != nil {
		return HandleOutput{}, outcome.Err
	}
	metadata := outcome.Response.Metadata()
	metadata.ModelRequested = intent.RequestedModel
	metadata.ModelResolved = route.EffectiveModel
	metadata.ModelResolutionMode = route.ResolutionMode
	resp := outcome.Response.WithMetadata(metadata)

	return HandleOutput{
		Response: resp,
		Target:   route.Target,
	}, nil
}

func (o RequestHandler) ListModels(ctx context.Context, in ListModelsInput) (ListModelsOutput, error) {
	if in.EndpointName.IsZero() {
		return ListModelsOutput{}, compatibility.BadEndpoint("endpoint name is required")
	}
	if o.endpoints == nil {
		return ListModelsOutput{}, compatibility.InternalError("endpoint reader is not configured")
	}
	endpoint, err := o.endpoints.GetEndpoint(ctx, in.EndpointName)
	if err != nil {
		return ListModelsOutput{}, compatibility.BadEndpoint("endpoint could not be resolved")
	}
	catalog := buildEndpointModelCatalog(endpoint)
	models := make([]ModelOption, 0, len(catalog.Entries)+1)
	if primary, ok := primaryModelOption(catalog); ok {
		models = append(models, primary)
	}
	for _, entry := range catalog.Entries {
		models = append(models, ModelOption{
			ID:           entry.ID,
			ModelID:      entry.ModelID,
			ProviderSpec: entry.ProviderSpec,
			BackendRef:   entry.ProviderRef,
		})
	}
	return ListModelsOutput{
		DefaultModelID: catalog.DefaultID,
		Models:         models,
	}, nil
}

func primaryModelOption(catalog endpointModelCatalog) (ModelOption, bool) {
	for _, entry := range catalog.Entries {
		if normalizeSelector(entry.ID) == compatibility.PrimaryTargetSelector {
			return ModelOption{}, false
		}
	}
	for _, entry := range catalog.Entries {
		if entry.ProviderRef != catalog.DefaultRef {
			continue
		}
		return ModelOption{
			ID:           compatibility.PrimaryTargetSelector,
			ModelID:      entry.ModelID,
			ProviderSpec: entry.ProviderSpec,
			BackendRef:   entry.ProviderRef,
		}, true
	}
	return ModelOption{}, false
}

func materializeRequestForExecution(request compatibility.CanonicalRequest, modelID string) compatibility.CanonicalRequest {
	if strings.TrimSpace(modelID) == "" {
		return request
	}
	switch typed := request.(type) {
	case compatibility.DialogCanonicalRequest:
		return compatibility.NewDialogRequest(strings.TrimSpace(modelID), typed.Items())
	case compatibility.GenerationCanonicalRequest:
		return compatibility.NewGenerationRequest(compatibility.GenerationRequestParams{
			Model:                strings.TrimSpace(modelID),
			Thread:               typed.Thread(),
			LastTurn:             typed.LastTurn(),
			PreviousResponseID:   typed.PreviousResponseID(),
			ConversationID:       typed.ConversationID(),
			ToolMode:             typed.ToolMode(),
			PromptCacheKey:       typed.PromptCacheKey(),
			PromptCacheRetention: typed.PromptCacheRetention(),
		})
	case compatibility.PromptCanonicalRequest:
		return compatibility.NewPromptRequest(strings.TrimSpace(modelID), typed.Prompt())
	default:
		return request
	}
}

func requestModel(request compatibility.CanonicalRequest) string {
	switch typed := request.(type) {
	case compatibility.DialogCanonicalRequest:
		return strings.TrimSpace(typed.Model())
	case compatibility.GenerationCanonicalRequest:
		return strings.TrimSpace(typed.Model())
	case compatibility.PromptCanonicalRequest:
		return strings.TrimSpace(typed.Model())
	default:
		return ""
	}
}

func effectiveModelIDForRequest(selectedModelID string) (string, error) {
	selectedModelID = strings.TrimSpace(selectedModelID)
	if selectedModelID != "" {
		return selectedModelID, nil
	}
	return "", compatibility.BadRequest("selected provider model is not configured")
}
