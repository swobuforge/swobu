package canonical

import "fmt"

// RequestPrelude is the validated, derived view of leading request-scoped
// context. It is never persisted.
type RequestPrelude struct {
	items        []CanonicalItem
	directives   []CanonicalItem
	declarations []CanonicalItem
	toolsFirst   bool
}

func (p RequestPrelude) Items() []CanonicalItem        { return cloneCanonicalItems(p.items) }
func (p RequestPrelude) Directives() []CanonicalItem   { return cloneCanonicalItems(p.directives) }
func (p RequestPrelude) Declarations() []CanonicalItem { return cloneCanonicalItems(p.declarations) }
func (p RequestPrelude) ToolsFirst() bool              { return p.toolsFirst }

// SplitRequestPrelude separates and validates the leading request-scoped
// context. A prelude has at most one directive/declaration band transition;
// request scope after history begins is invalid canonical order.
func SplitRequestPrelude(items []CanonicalItem) (RequestPrelude, []CanonicalItem, error) {
	split := 0
	for split < len(items) && itemScope(items[split]) == ContextScopeRequest {
		split++
	}
	for index := split; index < len(items); index++ {
		if itemScope(items[index]) == ContextScopeRequest {
			return RequestPrelude{}, nil, fmt.Errorf("canonical request-scoped context at position %d follows history", index)
		}
	}
	prelude := RequestPrelude{items: cloneCanonicalItems(items[:split])}
	firstBand := ""
	previousBand := ""
	transitions := 0
	for _, item := range prelude.items {
		band := ""
		if _, ok := item.Message(); ok {
			band = "directive"
			prelude.directives = append(prelude.directives, item.Clone())
		} else if _, ok := item.ToolDeclarations(); ok {
			band = "tools"
			prelude.declarations = append(prelude.declarations, item.Clone())
		} else {
			return RequestPrelude{}, nil, fmt.Errorf("canonical request prelude contains an unsupported context item")
		}
		if firstBand == "" {
			firstBand = band
		}
		if previousBand != "" && previousBand != band {
			transitions++
			if transitions > 1 {
				return RequestPrelude{}, nil, fmt.Errorf("canonical request prelude context bands must not alternate")
			}
		}
		previousBand = band
	}
	prelude.toolsFirst = firstBand == "tools"
	return prelude, cloneCanonicalItems(items[split:]), nil
}

// RetainedHistory removes request-scoped context while preserving the order of
// every historical item.
func RetainedHistory(items []CanonicalItem) []CanonicalItem {
	retained := make([]CanonicalItem, 0, len(items))
	for _, item := range items {
		if itemScope(item) != ContextScopeRequest {
			retained = append(retained, item.Clone())
		}
	}
	return retained
}

type resolvedTool struct {
	declaration ToolDeclaration
	parent      ToolKey
}

// ToolEnvironment is the validated derived read model for tools effective at
// one history position. It owns global lookup and rejects contradictory
// declaration ownership; it is never canonical state.
type ToolEnvironment struct {
	declarations ToolSet
	byKey        map[ToolKey]resolvedTool
}

func (e ToolEnvironment) Declarations() []ToolDeclaration {
	return e.declarations.Declarations()
}
func (e ToolEnvironment) IsEmpty() bool { return e.declarations.IsEmpty() }
func (e ToolEnvironment) Lookup(key ToolKey) (ToolDeclaration, bool) {
	resolved, ok := e.byKey[key]
	if !ok {
		return ToolDeclaration{}, false
	}
	return resolved.declaration.Clone(), true
}

// ToolEnvironmentAt folds declaration contributions effective strictly before
// one item position. Equivalent redeclarations with the same direct owner are
// idempotent; conflicting identities or owners are ambiguous.
func ToolEnvironmentAt(items []CanonicalItem, before int) (ToolEnvironment, error) {
	if before < 0 || before > len(items) {
		return ToolEnvironment{}, fmt.Errorf("canonical tool environment position is out of range")
	}
	var ordered []ToolDeclaration
	byKey := make(map[ToolKey]resolvedTool)
	var observe func(ToolDeclaration, ToolKey) error
	observe = func(declaration ToolDeclaration, parent ToolKey) error {
		key := declaration.Key()
		if prior, ok := byKey[key]; ok {
			if !prior.declaration.Equivalent(declaration) || prior.parent != parent {
				return fmt.Errorf("canonical tool environment has ambiguous declaration %q", key.String())
			}
			return nil
		}
		resolved := resolvedTool{
			declaration: declaration,
			parent:      parent,
		}
		byKey[key] = resolved
		if source, ok := declaration.MCP(); ok {
			for _, child := range source.Tools() {
				if err := observe(child, source.Key()); err != nil {
					return err
				}
			}
		}
		if namespace, ok := declaration.Namespace(); ok {
			for _, child := range namespace.Tools() {
				if err := observe(child, namespace.Key()); err != nil {
					return err
				}
			}
		}
		return nil
	}
	add := func(set ToolSet) error {
		for _, declaration := range set.Declarations() {
			key := declaration.Key()
			if prior, ok := byKey[key]; ok {
				if !prior.declaration.Equivalent(declaration) || !prior.parent.IsZero() {
					return fmt.Errorf("canonical tool environment has ambiguous declaration %q", key.String())
				}
				continue
			}
			if err := observe(declaration, ToolKey{}); err != nil {
				return err
			}
			// declaration is already detached by set.Declarations() above and is
			// the same value observe stored in byKey. Share it read-only rather
			// than re-cloning: the types are immutable (no setters), so a single
			// detach at the boundary accessor is sufficient. epic-50 task 070.
			ordered = append(ordered, declaration)
		}
		return nil
	}
	for index := 0; index < before; index++ {
		if declaration, ok := items[index].ToolDeclarations(); ok {
			if err := add(declaration.Tools()); err != nil {
				return ToolEnvironment{}, err
			}
		}
		if result, ok := items[index].ToolDiscoveryResult(); ok {
			if err := add(result.Tools()); err != nil {
				return ToolEnvironment{}, err
			}
		}
	}
	declarations, err := newToolSetOwned(ordered)
	if err != nil {
		return ToolEnvironment{}, err
	}
	return ToolEnvironment{declarations: declarations, byKey: byKey}, nil
}

// EffectiveTools returns the declaration environment at the end of a request.
// Callers must propagate ambiguity instead of silently degrading to no tools.
func EffectiveTools(request CanonicalRequest) (ToolEnvironment, error) {
	items := request.Items()
	return ToolEnvironmentAt(items, len(items))
}

// TransformToolContributions applies one structural declaration transformation
// to every declaration-bearing occurrence while preserving carrier metadata.
// Surviving declarations retain their ToolKey. Occurrence refinements survive
// only for exact callable keys present after the transformation.
func TransformToolContributions(
	request CanonicalRequest,
	transform func(ToolSet) (ToolSet, error),
) (CanonicalRequest, error) {
	if transform == nil {
		return CanonicalRequest{}, fmt.Errorf("canonical tool contribution transform is nil")
	}
	items := request.Items()
	for index, item := range items {
		if occurrence, ok := item.ToolDeclarations(); ok {
			tools, err := transform(occurrence.Tools())
			if err != nil {
				return CanonicalRequest{}, err
			}
			responses, err := retainResponsesToolRefinements(tools, occurrence.Responses())
			if err != nil {
				return CanonicalRequest{}, err
			}
			items[index], err = NewToolDeclarationsItemWithResponses(tools, occurrence.Scope(), responses)
			if err != nil {
				return CanonicalRequest{}, err
			}
			continue
		}
		if result, ok := item.ToolDiscoveryResult(); ok {
			tools, err := transform(result.Tools())
			if err != nil {
				return CanonicalRequest{}, err
			}
			responses, err := retainResponsesToolRefinements(tools, result.Responses())
			if err != nil {
				return CanonicalRequest{}, err
			}
			items[index], err = NewToolDiscoveryResultItemWithResponsesWireID(result.CallID(), tools, result.Executor(), responses, result.ResponsesCallIDNull())
			if err != nil {
				return CanonicalRequest{}, err
			}
		}
	}
	return request.WithItems(items), nil
}

func retainResponsesToolRefinements(after ToolSet, refinements ResponsesToolRefinements) (ResponsesToolRefinements, error) {
	deferred := refinements.DeferredKeys()
	if len(deferred) == 0 {
		return ResponsesToolRefinements{}, nil
	}
	known := callableToolKeys(after)
	retained := make([]ToolKey, 0, len(deferred))
	for _, key := range deferred {
		if _, ok := known[key]; ok {
			retained = append(retained, key)
		}
	}
	return NewResponsesToolRefinements(after, retained)
}

func itemScope(item CanonicalItem) ContextScope {
	if message, ok := item.Message(); ok {
		return message.Scope()
	}
	if declarations, ok := item.ToolDeclarations(); ok {
		return declarations.Scope()
	}
	return ContextScopeHistory
}
