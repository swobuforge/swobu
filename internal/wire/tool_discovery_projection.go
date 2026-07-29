package wire

import (
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/provider"
)

// StaticToolSet is the canonical request shape prepared for a target that
// cannot execute dynamic discovery.
type StaticToolSet struct {
	Items               []canonical.CanonicalItem
	Declarations        []canonical.ToolDeclaration
	RemovedEffects      int
	RemovedDeclarations int
}

// PrepareStaticToolSet omits completed discovery effects from a one-way
// backend request while retaining the declarations they loaded in the
// separately supplied effective environment. A bare declaration, unresolved
// call, orphan result, or unmaterialized returned declaration is live
// capability and makes the static target incompatible.
func PrepareStaticToolSet(
	items []canonical.CanonicalItem,
	declarations []canonical.ToolDeclaration,
) (StaticToolSet, error) {
	completedIndexes := make(map[int]struct{})
	completedDiscovery := make([]canonical.ToolEffect, 0)
	var matcher canonical.ToolEffectMatcher
	for index, item := range items {
		if call, ok := item.ToolCall(); ok && call.Tool().Kind() != canonical.ToolKindDiscovery {
			continue
		}
		if _, call := item.ToolCall(); !call {
			if _, result := item.ToolDiscoveryResult(); !result {
				continue
			}
		}
		completed, err := matcher.Accept(index, item)
		if err != nil {
			return StaticToolSet{}, provider.NewIncompatibleTarget(err.Error())
		}
		if completed == nil {
			continue
		}
		completedDiscovery = append(completedDiscovery, *completed)
		completedIndexes[completed.CallIndex] = struct{}{}
		completedIndexes[completed.ResultIndex] = struct{}{}
	}
	hasDiscoveryDeclaration := false
	materialized := make(map[canonical.ToolKey]canonical.ToolDeclaration, len(declarations))
	for _, declaration := range declarations {
		materialized[declaration.Key()] = declaration
		if declaration.Kind() == canonical.ToolKindDiscovery {
			hasDiscoveryDeclaration = true
		}
	}
	discoverySources := make([]int, 0)
	for index, item := range items {
		if contribution, ok := item.ToolDeclarations(); ok {
			for _, declaration := range contribution.Tools().Declarations() {
				if declaration.Kind() != canonical.ToolKindDiscovery {
					continue
				}
				if contribution.Scope() != canonical.ContextScopeHistory {
					return StaticToolSet{}, provider.NewIncompatibleTarget("static target cannot remove a request-scoped canonical discovery capability")
				}
				discoverySources = append(discoverySources, index)
			}
		}
		if call, ok := item.ToolCall(); ok && call.Tool().Kind() == canonical.ToolKindDiscovery {
			if _, completed := completedIndexes[index]; !completed {
				return StaticToolSet{}, provider.NewIncompatibleTarget("static target cannot represent an unresolved canonical discovery call")
			}
		}
		if result, ok := item.ToolDiscoveryResult(); ok {
			if _, completed := completedIndexes[index]; !completed {
				return StaticToolSet{}, provider.NewIncompatibleTarget("static target cannot represent an orphan canonical discovery result")
			}
			for _, loaded := range result.Tools().Declarations() {
				declaration, present := materialized[loaded.Key()]
				if !present || !declaration.Equivalent(loaded) {
					return StaticToolSet{}, provider.NewIncompatibleTarget("static target cannot omit discovery before its returned declarations are materialized")
				}
				if loaded.Kind() == canonical.ToolKindDiscovery {
					discoverySources = append(discoverySources, index)
				}
			}
		}
	}
	if hasDiscoveryDeclaration {
		if len(discoverySources) == 0 {
			return StaticToolSet{}, provider.NewIncompatibleTarget("static target cannot prove the source of a canonical discovery capability")
		}
		for _, sourceIndex := range discoverySources {
			consumed := false
			for _, effect := range completedDiscovery {
				if effect.CallIndex > sourceIndex {
					consumed = true
					break
				}
			}
			if !consumed {
				return StaticToolSet{}, provider.NewIncompatibleTarget("static target cannot represent a live canonical discovery capability")
			}
		}
	}
	projectedItems := make([]canonical.CanonicalItem, 0, len(items))
	omittedLifecycles := 0
	for index, item := range items {
		if _, completed := completedIndexes[index]; completed {
			if _, result := item.ToolDiscoveryResult(); result {
				omittedLifecycles++
			}
			continue
		}
		projectedItems = append(projectedItems, item)
	}
	projectedDeclarations := make([]canonical.ToolDeclaration, 0, len(declarations))
	omittedDeclarations := 0
	for _, declaration := range declarations {
		if declaration.Kind() == canonical.ToolKindDiscovery {
			omittedDeclarations++
			continue
		}
		projectedDeclarations = append(projectedDeclarations, declaration)
	}
	return StaticToolSet{
		Items:               projectedItems,
		Declarations:        projectedDeclarations,
		RemovedEffects:      omittedLifecycles,
		RemovedDeclarations: omittedDeclarations,
	}, nil
}
