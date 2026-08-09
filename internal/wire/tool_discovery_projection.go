package wire

import (
	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// ToolDiscoveryProjection is one attempt-local portable request projection.
// StructuralHistoryChange forbids native continuation because the projected
// item sequence no longer matches the provider's stored history.
type ToolDiscoveryProjection struct {
	Request                 canonical.CanonicalRequest
	Changes                 []compat.Change
	Changed                 bool
	StructuralHistoryChange bool
}

// ProjectToolDiscoveryPolyfill replaces dynamic discovery with the final known
// eager tool environment. Durable canonical truth remains unchanged; exchange
// uses the returned request only for one provider attempt and its decode context.
func ProjectToolDiscoveryPolyfill(request canonical.CanonicalRequest) (ToolDiscoveryProjection, error) {
	items := request.Items()
	effects, err := canonical.MatchToolEffects(items)
	if err != nil {
		return ToolDiscoveryProjection{}, err
	}
	completedDiscovery := make(map[int]struct{})
	pendingDiscoveryIndex := -1
	for _, effect := range effects {
		if effect.Kind != canonical.ToolKindDiscovery {
			continue
		}
		if effect.ResultIndex < 0 {
			pendingDiscoveryIndex = effect.CallIndex
			continue
		}
		completedDiscovery[effect.CallIndex] = struct{}{}
		completedDiscovery[effect.ResultIndex] = struct{}{}
	}
	if pendingDiscoveryIndex >= 0 {
		return ToolDiscoveryProjection{}, provider.IncompatibleCapability(
			canonical.RequestItemsKind,
			canonical.RequestItemOccurrence(uint32(pendingDiscoveryIndex)),
			"portable tool-discovery projection cannot erase an unresolved discovery call",
		)
	}

	environment, err := canonical.EffectiveTools(request)
	if err != nil {
		return ToolDiscoveryProjection{}, err
	}
	declarations := environment.Declarations()
	portable := make([]canonical.ToolDeclaration, 0, len(declarations))
	hasDiscoveryDeclaration := false
	clientDiscoveryDeclared := false
	for _, declaration := range declarations {
		if declaration.Kind() == canonical.ToolKindDiscovery {
			hasDiscoveryDeclaration = true
			discovery, _ := declaration.Discovery()
			clientDiscoveryDeclared = clientDiscoveryDeclared || discovery.Executor() == canonical.DiscoveryExecutorClient
			continue
		}
		portable = append(portable, declaration)
	}
	if clientDiscoveryDeclared {
		return ToolDiscoveryProjection{}, provider.IncompatibleCapability(
			canonical.RequestToolsKind,
			canonical.ToolOccurrence(canonical.ToolDiscoveryKey()),
			"portable tool-discovery projection cannot replace live client discovery inventory",
		)
	}

	hasDeferred := HasDeferredTools(items)
	hasHistoricalDeclarations := false
	hasDiscoveryItem := false
	for _, item := range items {
		if declaration, ok := item.ToolDeclarations(); ok {
			if declaration.Scope() == canonical.ContextScopeHistory {
				hasHistoricalDeclarations = true
			}
		}
		if call, ok := item.ToolCall(); ok && call.Tool().Kind() == canonical.ToolKindDiscovery {
			hasDiscoveryItem = true
		}
		if result, ok := item.ToolDiscoveryResult(); ok {
			hasDiscoveryItem = true
			_ = result
		}
	}

	needsProjection := hasDiscoveryDeclaration || hasDiscoveryItem || hasDeferred || hasHistoricalDeclarations
	if !needsProjection {
		return ToolDiscoveryProjection{Request: request.Clone()}, nil
	}
	for index, item := range items {
		result, ok := item.ToolDiscoveryResult()
		if !ok {
			continue
		}
		if _, completed := completedDiscovery[index]; !completed {
			return ToolDiscoveryProjection{}, provider.IncompatibleCapability(
				canonical.RequestItemsKind,
				canonical.RequestItemOccurrence(uint32(index)),
				"portable tool-discovery projection cannot represent an orphan discovery result",
			)
		}
		for _, loaded := range result.Tools().Declarations() {
			if loaded.Kind() == canonical.ToolKindDiscovery {
				continue
			}
			resolved, present := environment.Lookup(loaded.Key())
			if !present || !resolved.Equivalent(loaded) {
				return ToolDiscoveryProjection{}, provider.IncompatibleCapability(
					canonical.RequestTools,
					canonical.RequestItemOccurrence(uint32(index)),
					"portable tool-discovery projection cannot omit discovery before its returned declarations are materialized",
				)
			}
		}
	}

	policy, err := request.EffectiveToolPolicy()
	if err != nil {
		return ToolDiscoveryProjection{}, err
	}
	if err := policy.ValidateForTools(portable); err != nil {
		return ToolDiscoveryProjection{}, provider.IncompatibleCapability(
			canonical.RequestToolPolicy,
			canonical.Occurrence{},
			"portable tool-discovery projection cannot preserve the canonical tool-selection constraint",
		)
	}

	prelude, history, err := canonical.SplitRequestPrelude(items)
	if err != nil {
		return ToolDiscoveryProjection{}, err
	}
	retainedHistory := make([]canonical.CanonicalItem, 0, len(history))
	for _, item := range history {
		if _, ok := item.ToolDeclarations(); ok {
			continue
		}
		if call, ok := item.ToolCall(); ok && call.Tool().Kind() == canonical.ToolKindDiscovery {
			continue
		}
		if _, ok := item.ToolDiscoveryResult(); ok {
			continue
		}
		retainedHistory = append(retainedHistory, item.Clone())
	}
	requestDirectives := prelude.Directives()
	projectedItems := make([]canonical.CanonicalItem, 0, len(requestDirectives)+len(retainedHistory)+1)
	var declarationItem canonical.CanonicalItem
	hasPortableDeclarations := len(portable) > 0
	if hasPortableDeclarations {
		tools, err := canonical.NewToolSet(portable)
		if err != nil {
			return ToolDiscoveryProjection{}, err
		}
		declarationItem, err = canonical.NewToolDeclarationsItem(tools, canonical.ContextScopeRequest)
		if err != nil {
			return ToolDiscoveryProjection{}, err
		}
	}
	if hasPortableDeclarations && prelude.ToolsFirst() {
		projectedItems = append(projectedItems, declarationItem)
	}
	projectedItems = append(projectedItems, requestDirectives...)
	if hasPortableDeclarations && !prelude.ToolsFirst() {
		projectedItems = append(projectedItems, declarationItem)
	}
	projectedItems = append(projectedItems, retainedHistory...)

	changes := make([]compat.Change, 0, 3)
	if hasDiscoveryItem {
		changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestItemsKind, canonical.RequestTools, canonical.Occurrence{}))
	}
	if hasDiscoveryDeclaration {
		changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestToolsKind, canonical.RequestTools, canonical.Occurrence{}))
	}
	if hasHistoricalDeclarations {
		changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestTools, canonical.RequestTools, canonical.Occurrence{}))
	}
	if hasDeferred {
		changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestToolsVisibility, canonical.RequestTools, canonical.Occurrence{}))
	}
	return ToolDiscoveryProjection{
		Request:                 request.WithItems(projectedItems),
		Changes:                 changes,
		Changed:                 true,
		StructuralHistoryChange: hasHistoricalDeclarations || hasDiscoveryItem,
	}, nil
}
