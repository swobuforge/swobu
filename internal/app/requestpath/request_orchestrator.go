// protocol request lifecycle in one application seam.
package requestpath

import (
	"context"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
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
// canonical request objects so semantic types stay semantic-only.
type HandleInput struct {
	EndpointName endpointintent.EndpointName
	RequestID    string
	Request      canonical.CanonicalRequest
	Contract     ExecutionContract
	Provenance   IngressProvenance
}

// IngressProvenance captures normalized, metadata-only ingress context carried
// from the HTTP edge into evidence truth.
type IngressProvenance struct {
	ClientProtocol string
	IngressFamily  canonical.IngressFamily
	NormalizedOp   canonical.NormalizedPath
	ClientHandler  string
}

// HandleOutput returns the selected target plus the semantic provider
// response. The app layer does not rewrite successful semantics; that remains a
// client-edge responsibility in the inbound adapter.
type HandleOutput struct {
	Response ports.ProviderResponse
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

// Handle resolves durable intent, prepares any
// continuation-aware request views, emits runtime evidence, and delegates
// semantic execution to the provider port. It preserves Swobu-vs-backend error
// origin rather than laundering failures into one generic class.
// resolution, continuation prep, provider execution, and evidence emission.
// independent failure and delivery paths that belong in one orchestrator.
func (o RequestHandler) Handle(ctx context.Context, in HandleInput) (HandleOutput, error) {
	if in.EndpointName.IsZero() {
		return HandleOutput{}, canonical.BadEndpoint("endpoint name is required")
	}
	if in.Request == nil {
		return HandleOutput{}, canonical.BadRequest("canonical request is required")
	}
	if o.endpoints == nil {
		return HandleOutput{}, canonical.InternalError("endpoint reader is not configured")
	}
	if o.providers == nil {
		return HandleOutput{}, canonical.InternalError("provider executor is not configured")
	}
	if o.routingPolicy == nil {
		return HandleOutput{}, canonical.InternalError("routing policy is not configured")
	}

	endpoint, err := o.endpoints.GetEndpoint(ctx, in.EndpointName)
	if err != nil {
		return HandleOutput{}, canonical.BadEndpoint("endpoint could not be resolved")
	}

	intent := ClientIntent{
		EndpointName:   in.EndpointName,
		RequestID:      in.RequestID,
		Request:        canonical.CloneCanonicalRequest(in.Request),
		RequestedModel: requestModel(in.Request),
		Provenance:     in.Provenance,
	}
	route, err := o.routingPolicy.Decide(ctx, endpoint, intent)
	if err != nil {
		return HandleOutput{}, err
	}
	routes := []RouteDecision{route}
	if in.Contract.AllowPreCommitFallback {
		allRoutes, fallbackErr := fallbackRouteDecisions(endpoint, intent.RequestedModel)
		if fallbackErr != nil {
			return HandleOutput{}, fallbackErr
		}
		if len(allRoutes) > 0 {
			routes = allRoutes
		}
	}
	var (
		outcome    AttemptOutcome
		lastErr    error
		finalRoute RouteDecision
	)
	for i, candidate := range routes {
		resolvedRequest := materializeRequestForExecution(intent.Request, candidate.EffectiveModel)
		protocolKind, err := resolveProviderProtocolForRequest(candidate.Target.ProviderID(), candidate.Target.ProtocolKind, resolvedRequest)
		if err != nil {
			return HandleOutput{}, err
		}
		candidate.Target.ProtocolKind = protocolKind
		backendModel := BackendModelEntity{
			BackendRef:     candidate.Target.BackendRef,
			ProviderSpec:   candidate.Target.ProviderID(),
			ProtocolKind:   candidate.Target.ProtocolKind,
			BackendModelID: candidate.EffectiveModel,
		}
		attemptContract := in.Contract
		providerCallMode, err := planProviderCallMode(in.Contract.ClientResponseMode, candidate.Target)
		if err != nil {
			return HandleOutput{}, err
		}
		attemptContract = attemptContract.WithProviderCallMode(providerCallMode)
		outcome = o.executeAttempt(ctx, ExecutionAttempt{
			Intent:               intent,
			Route:                candidate,
			Index:                i + 1,
			Contract:             attemptContract,
			Request:              resolvedRequest,
			DeclaredCapabilities: o.capabilityCatalog.SnapshotFor(backendModel),
			Continuation:         canonical.NewContinuationRuntime(o.continuity),
		})
		if outcome.Err == nil {
			finalRoute = candidate
			break
		}
		lastErr = outcome.Err
	}
	if outcome.Err != nil {
		return HandleOutput{}, lastErr
	}
	metadata := outcome.Response.Metadata()
	metadata.ModelRequested = intent.RequestedModel
	metadata.ModelResolved = finalRoute.EffectiveModel
	metadata.ModelResolutionMode = finalRoute.ResolutionMode
	finalProviderCallMode, err := planProviderCallMode(in.Contract.ClientResponseMode, finalRoute.Target)
	if err != nil {
		return HandleOutput{}, err
	}
	finalContract := in.Contract.WithProviderCallMode(finalProviderCallMode)
	metadata.ClientResponseMode = finalContract.ClientResponseMode.String()
	metadata.ProviderCallMode = finalContract.ProviderCallMode.String()
	metadata.ConversionKind = finalContract.ConversionKind.String()
	resp := outcome.Response.WithMetadata(metadata)

	return HandleOutput{
		Response: resp,
		Target:   finalRoute.Target,
	}, nil
}

func (o RequestHandler) ListModels(ctx context.Context, in ListModelsInput) (ListModelsOutput, error) {
	if in.EndpointName.IsZero() {
		return ListModelsOutput{}, canonical.BadEndpoint("endpoint name is required")
	}
	if o.endpoints == nil {
		return ListModelsOutput{}, canonical.InternalError("endpoint reader is not configured")
	}
	endpoint, err := o.endpoints.GetEndpoint(ctx, in.EndpointName)
	if err != nil {
		return ListModelsOutput{}, canonical.BadEndpoint("endpoint could not be resolved")
	}
	selected := endpoint.SelectedProviderConfig()
	models := []ModelOption{{
		ID:           PublicModelIDSwobu,
		ModelID:      selected.ModelID(),
		ProviderSpec: selected.ProviderSpec().String(),
		BackendRef:   selected.Ref().String(),
	}}
	return ListModelsOutput{
		DefaultModelID: PublicModelIDSwobu,
		Models:         models,
	}, nil
}

func materializeRequestForExecution(request canonical.CanonicalRequest, modelID string) canonical.CanonicalRequest {
	if strings.TrimSpace(modelID) == "" { // trimlowerlint:allow boundary canonicalization
		return request
	}
	switch typed := request.(type) {
	case canonical.DialogCanonicalRequest:
		return canonical.NewDialogRequest(strings.TrimSpace(modelID), typed.Items()) // trimlowerlint:allow boundary canonicalization
	case canonical.GenerationCanonicalRequest:
		return canonical.NewGenerationRequest(canonical.GenerationRequestParams{
			Model:                strings.TrimSpace(modelID), // trimlowerlint:allow boundary canonicalization
			Thread:               typed.Thread(),
			LastTurn:             typed.LastTurn(),
			PreviousResponseID:   typed.PreviousResponseID(),
			ConversationID:       typed.ConversationID(),
			ToolMode:             typed.ToolMode(),
			PromptCacheKey:       typed.PromptCacheKey(),
			PromptCacheRetention: typed.PromptCacheRetention(),
		})
	case canonical.PromptCanonicalRequest:
		return canonical.NewPromptRequest(strings.TrimSpace(modelID), typed.Prompt()) // trimlowerlint:allow boundary canonicalization
	default:
		return request
	}
}

func requestModel(request canonical.CanonicalRequest) string {
	switch typed := request.(type) {
	case canonical.DialogCanonicalRequest:
		return strings.TrimSpace(typed.Model()) // trimlowerlint:allow boundary canonicalization
	case canonical.GenerationCanonicalRequest:
		return strings.TrimSpace(typed.Model()) // trimlowerlint:allow boundary canonicalization
	case canonical.PromptCanonicalRequest:
		return strings.TrimSpace(typed.Model()) // trimlowerlint:allow boundary canonicalization
	default:
		return ""
	}
}

func effectiveModelIDForRequest(selectedModelID string) (string, error) {
	selectedModelID = strings.TrimSpace(selectedModelID) // trimlowerlint:allow boundary canonicalization
	if selectedModelID != "" {
		return selectedModelID, nil
	}
	return "", canonical.BadRequest("selected provider model is not configured")
}
