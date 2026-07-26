package provider

import (
	"fmt"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire/toolname"
)

// ToolProjectionTable is one attempted request's bounded flat-name bijection.
// Provider responses resolve only through this exact table.
type ToolProjectionTable struct {
	byCanonical map[canonical.ToolKey]string
	byWire      map[wireToolKey]canonical.ToolKey
}

// ToolProjection is the single alias authority for one provider attempt.
// Every request view and the response decoder use this same immutable table.
type ToolProjection struct {
	table                ToolProjectionTable
	declarationDecisions []compat.Decision
	declared             map[canonical.ToolKey]bool
}

type wireToolKey struct {
	kind canonical.ToolKind
	name string
}

func (t ToolProjectionTable) WireName(key canonical.ToolKey) (string, error) {
	name, ok := t.byCanonical[key]
	if !ok {
		return "", fmt.Errorf("attempt tool projection has no canonical key %q", key.String())
	}
	return name, nil
}

func (t ToolProjectionTable) CanonicalKey(kind canonical.ToolKind, wireName string) (canonical.ToolKey, bool) {
	key, ok := t.byWire[wireToolKey{kind: kind, name: wireName}]
	return key.Clone(), ok
}

func (t ToolProjectionTable) OriginalKey(attemptKey canonical.ToolKey) (canonical.ToolKey, bool) {
	return t.CanonicalKey(attemptKey.Kind(), attemptKey.Name())
}

func (t *ToolProjectionTable) bindWire(key canonical.ToolKey, wire string) {
	t.byWire[wireToolKey{kind: key.Kind(), name: wire}] = key.Clone()
}

// ProjectAttemptTools replaces declaration/call keys with request-scoped wire
// aliases while retaining the reverse table outside checkpoint truth.
func ProjectAttemptTools(request canonical.CanonicalRequest) (canonical.CanonicalRequest, ToolProjectionTable, []compat.Decision, error) {
	projection, err := BuildToolProjection(request)
	if err != nil {
		return canonical.CanonicalRequest{}, ToolProjectionTable{}, nil, err
	}
	projected, decisions, err := projection.Rewrite(request)
	return projected, projection.Table(), decisions, err
}

// BuildToolProjection allocates aliases once from the complete semantic tool
// environment. Rewrite applies that closed decision to any view of the attempt.
func BuildToolProjection(semantic canonical.CanonicalRequest) (ToolProjection, error) {
	table := ToolProjectionTable{byCanonical: map[canonical.ToolKey]string{}, byWire: map[wireToolKey]canonical.ToolKey{}}
	decisions := make([]compat.Decision, 0)
	environment, err := canonical.ToolEnvironmentAt(semantic.Items(), len(semantic.Items()))
	if err != nil {
		return ToolProjection{}, err
	}
	declarations := environment.Declarations()
	keys := make([]canonical.ToolKey, 0, len(declarations))
	for _, declaration := range declarations {
		keys = appendProjectionDeclarationKeys(keys, declaration)
	}
	for _, item := range semantic.Items() {
		if call, ok := item.ToolCall(); ok {
			keys = append(keys, call.Tool())
		}
	}
	if key, ok := semantic.ToolPolicy().SpecificID(); ok {
		keys = append(keys, key)
	}
	// Legal literal request names have priority because changing them would be
	// an avoidable semantic projection.
	for _, key := range keys {
		if _, exists := table.byCanonical[key]; exists || key.Namespace() != canonical.ToolNamespaceRequest || !toolname.Safe(key.Name()) {
			continue
		}
		wire := key.Name()
		table.byCanonical[key] = wire
		table.bindWire(key, wire)
	}
	for _, key := range keys {
		if _, exists := table.byCanonical[key]; exists {
			continue
		}
		var wire string
		leafIndex := wireToolKey{kind: key.Kind(), name: key.Name()}
		if toolname.Safe(key.Name()) {
			if _, occupied := table.byWire[leafIndex]; !occupied {
				wire = key.Name()
				table.bindWire(key, wire)
			}
		}
		if wire == "" {
			for ordinal := uint32(0); ; ordinal++ {
				wire = toolname.Alias(key.String(), key.Name(), ordinal)
				index := wireToolKey{kind: key.Kind(), name: wire}
				if _, occupied := table.byWire[index]; !occupied {
					table.bindWire(key, wire)
					break
				}
				if ordinal == ^uint32(0) {
					return ToolProjection{}, fmt.Errorf("attempt tool projection alias space exhausted")
				}
			}
		}
		table.byCanonical[key] = wire
	}
	declared := map[canonical.ToolKey]bool{}
	for _, declaration := range declarations {
		recordProjectedDeclaration(declaration, table, declared, &decisions)
	}
	return ToolProjection{
		table: table, declarationDecisions: decisions, declared: declared,
	}, nil
}

func (p ToolProjection) Table() ToolProjectionTable { return p.table }

func (p ToolProjection) Rewrite(request canonical.CanonicalRequest) (canonical.CanonicalRequest, []compat.Decision, error) {
	decisions := append([]compat.Decision(nil), p.declarationDecisions...)
	for _, item := range request.Items() {
		if call, ok := item.ToolCall(); ok && !p.declared[call.Tool()] {
			if wire, _ := p.table.WireName(call.Tool()); wire != call.Tool().Name() {
				decisions = append(decisions, compat.Decision{Feature: compat.RequestItemsToolCallName, Outcome: compat.Approx, Subject: compat.Subject(call.Tool().String())})
			}
		}
	}
	projected, err := rewriteAttemptToolKeys(request, p.table)
	return projected, decisions, err
}

func appendProjectionDeclarationKeys(keys []canonical.ToolKey, declaration canonical.ToolDeclaration) []canonical.ToolKey {
	switch declaration.Kind() {
	case canonical.ToolKindWebSearch, canonical.ToolKindDiscovery:
		return keys
	}
	keys = append(keys, declaration.Key())
	if namespace, ok := declaration.Namespace(); ok {
		for _, child := range namespace.Tools() {
			keys = appendProjectionDeclarationKeys(keys, child)
		}
	}
	return keys
}

func recordProjectedDeclaration(declaration canonical.ToolDeclaration, table ToolProjectionTable, declared map[canonical.ToolKey]bool, decisions *[]compat.Decision) {
	declared[declaration.Key()] = true
	if declaration.Kind() != canonical.ToolKindWebSearch && declaration.Kind() != canonical.ToolKindDiscovery {
		if wire, _ := table.WireName(declaration.Key()); wire != declaration.Key().Name() {
			*decisions = append(*decisions, compat.Decision{Feature: compat.RequestToolsName, Outcome: compat.Approx, Subject: compat.Subject(declaration.Key().String())})
		}
	}
	if namespace, ok := declaration.Namespace(); ok {
		for _, child := range namespace.Tools() {
			recordProjectedDeclaration(child, table, declared, decisions)
		}
	}
}

func rewriteAttemptToolKeys(request canonical.CanonicalRequest, table ToolProjectionTable) (canonical.CanonicalRequest, error) {
	projectDeclarations := func(declarations []canonical.ToolDeclaration) ([]canonical.ToolDeclaration, error) {
		var projectOne func(canonical.ToolDeclaration) (canonical.ToolDeclaration, error)
		projectOne = func(declaration canonical.ToolDeclaration) (canonical.ToolDeclaration, error) {
			if declaration.Kind() == canonical.ToolKindWebSearch {
				return canonical.NewWebSearchDeclaration(), nil
			}
			if discovery, ok := declaration.Discovery(); ok {
				return canonical.NewToolDiscoveryTool(discovery.Description(), discovery.InputSchema(), discovery.Executor())
			}
			wire, _ := table.WireName(declaration.Key())
			key, err := canonical.NewToolKey(canonical.ToolNamespaceRequest, declaration.Kind(), wire)
			if err != nil {
				return canonical.ToolDeclaration{}, err
			}
			if function, ok := declaration.Function(); ok {
				return canonical.NewFunctionTool(key, function.Description(), function.InputSchema(), function.Strict())
			} else if custom, ok := declaration.Custom(); ok {
				return canonical.NewCustomTool(key, custom.Description(), custom.Format())
			}
			if namespace, ok := declaration.Namespace(); ok {
				children := make([]canonical.ToolDeclaration, len(namespace.Tools()))
				for i, child := range namespace.Tools() {
					children[i], err = projectOne(child)
					if err != nil {
						return canonical.ToolDeclaration{}, err
					}
				}
				return canonical.NewToolNamespace(key, namespace.Description(), children)
			}
			return canonical.ToolDeclaration{}, canonical.InternalError("provider attempt projection encountered an invalid tool declaration")
		}
		projectedDeclarations := make([]canonical.ToolDeclaration, 0, len(declarations))
		for _, declaration := range declarations {
			projected, err := projectOne(declaration)
			if err != nil {
				return nil, err
			}
			projectedDeclarations = append(projectedDeclarations, projected)
		}
		return projectedDeclarations, nil
	}
	rewritten, err := canonical.RewriteToolContributions(request, func(set canonical.ToolSet) (canonical.ToolSet, error) {
		projected, err := projectDeclarations(set.Declarations())
		if err != nil {
			return canonical.ToolSet{}, err
		}
		return canonical.NewToolSet(projected)
	})
	if err != nil {
		return canonical.CanonicalRequest{}, err
	}
	items := rewritten.Items()
	for i, item := range items {
		call, ok := item.ToolCall()
		if !ok {
			continue
		}
		if call.Tool().Kind() == canonical.ToolKindWebSearch || call.Tool().Kind() == canonical.ToolKindDiscovery {
			continue
		}
		wire, _ := table.WireName(call.Tool())
		key, err := canonical.NewToolKey(canonical.ToolNamespaceRequest, call.Tool().Kind(), wire)
		if err != nil {
			return canonical.CanonicalRequest{}, err
		}
		items[i], err = canonical.NewToolCallItem(call.CallID(), key, call.Input())
		if err != nil {
			return canonical.CanonicalRequest{}, err
		}
	}
	toolPolicy := request.ToolPolicyField()
	if policy, specified := toolPolicy.Get(); specified {
		if key, ok := policy.SpecificID(); ok {
			wire, _ := table.WireName(key)
			projected, err := canonical.NewToolKey(canonical.ToolNamespaceRequest, key.Kind(), wire)
			if err != nil {
				return canonical.CanonicalRequest{}, err
			}
			policy = canonical.NewToolPolicy(canonical.ToolPolicySpecific, &projected)
		}
		toolPolicy = canonical.Specify(policy)
	}
	params := canonical.RequestParams{
		Model: request.ModelField(), Items: items,
		ToolPolicy: toolPolicy, ToolCallBatch: request.ToolCallBatchField(), Controls: request.Controls(),
		Reasoning: request.Reasoning(), OutputFormat: request.OutputFormatField(),
	}
	if previous, ok := request.PreviousResponse(); ok {
		params.PreviousResponse = &previous
	}
	return canonical.NewCanonicalRequest(params), nil
}
