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
	completedEffects := make([]canonical.ToolEffect, 0)
	pendingDiscovery := false
	for _, effect := range effects {
		if effect.Kind != canonical.ToolKindDiscovery {
			continue
		}
		if effect.ResultIndex < 0 {
			pendingDiscovery = true
			continue
		}
		completedDiscovery[effect.CallIndex] = struct{}{}
		completedDiscovery[effect.ResultIndex] = struct{}{}
		completedEffects = append(completedEffects, effect)
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

	hasDeferred := HasDeferredTools(items)
	hasHistoricalDeclarations := false
	hasDeclarationContribution := false
	hasDiscoveryItem := false
	discoverySources := make([]struct {
		index    int
		executor canonical.DiscoveryExecutor
	}, 0)
	for index, item := range items {
		if declaration, ok := item.ToolDeclarations(); ok {
			hasDeclarationContribution = true
			if declaration.Scope() == canonical.ContextScopeHistory {
				hasHistoricalDeclarations = true
			}
			for _, tool := range declaration.Tools().Declarations() {
				if discovery, ok := tool.Discovery(); ok {
					discoverySources = append(discoverySources, struct {
						index    int
						executor canonical.DiscoveryExecutor
					}{index: index, executor: discovery.Executor()})
				}
			}
		}
		if call, ok := item.ToolCall(); ok && call.Tool().Kind() == canonical.ToolKindDiscovery {
			hasDiscoveryItem = true
			if _, completed := completedDiscovery[index]; !completed {
				pendingDiscovery = true
			}
		}
		if result, ok := item.ToolDiscoveryResult(); ok {
			hasDiscoveryItem = true
			for _, tool := range result.Tools().Declarations() {
				if discovery, ok := tool.Discovery(); ok {
					discoverySources = append(discoverySources, struct {
						index    int
						executor canonical.DiscoveryExecutor
					}{index: index, executor: discovery.Executor()})
				}
			}
		}
	}

	needsProjection := hasDiscoveryDeclaration || hasDiscoveryItem || hasDeferred || hasHistoricalDeclarations
	if !needsProjection {
		return ToolDiscoveryProjection{Request: request.Clone()}, nil
	}
	for _, source := range discoverySources {
		consumed := false
		for _, effect := range completedEffects {
			executor, _ := effect.Executor.Get()
			if effect.CallIndex > source.index && executor == source.executor {
				consumed = true
				break
			}
		}
		if source.executor == canonical.DiscoveryExecutorClient && !consumed {
			return ToolDiscoveryProjection{}, provider.IncompatibleCapability(
				canonical.RequestTools,
				canonical.RequestItemOccurrence(uint32(source.index)),
				"portable tool-discovery projection cannot preserve live client discovery inventory",
			)
		}
	}
	if clientDiscoveryDeclared && len(discoverySources) == 0 {
		return ToolDiscoveryProjection{}, provider.IncompatibleCapability(
			canonical.RequestTools,
			canonical.Occurrence{},
			"portable tool-discovery projection cannot prove the source of live client discovery inventory",
		)
	}
	if pendingDiscovery && len(portable) == 0 {
		return ToolDiscoveryProjection{}, provider.IncompatibleCapability(
			canonical.RequestTools,
			canonical.Occurrence{},
			"portable tool-discovery projection has no materialized callable catalog",
		)
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
	if hasDiscoveryDeclaration || hasDiscoveryItem {
		changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestItemsKind, canonical.RequestTools, canonical.Occurrence{}))
	}
	if hasDeclarationContribution {
		changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestTools, canonical.RequestTools, canonical.Occurrence{}))
	}
	if hasDeferred {
		changes = compat.AppendUnique(changes, compat.NewApproximation(canonical.RequestToolsVisibility, canonical.RequestTools, canonical.Occurrence{}))
	}
	return ToolDiscoveryProjection{
		Request:                 request.WithItems(projectedItems),
		Changes:                 changes,
		StructuralHistoryChange: hasHistoricalDeclarations || hasDiscoveryItem,
	}, nil
}
