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

// ProjectAttemptTools replaces declaration/call keys with request-scoped wire
// aliases while retaining the reverse table outside checkpoint truth.
func ProjectAttemptTools(request canonical.CanonicalRequest) (canonical.CanonicalRequest, ToolProjectionTable, []compat.Decision, error) {
	table := ToolProjectionTable{byCanonical: map[canonical.ToolKey]string{}, byWire: map[wireToolKey]canonical.ToolKey{}}
	decisions := make([]compat.Decision, 0)
	keys := make([]canonical.ToolKey, 0, len(request.Tools()))
	for _, declaration := range request.Tools() {
		keys = append(keys, declaration.Key())
	}
	for _, item := range request.Items() {
		if call, ok := item.ToolCall(); ok {
			keys = append(keys, call.Tool())
		}
	}
	if key, ok := request.ToolPolicy().SpecificID(); ok {
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
		table.byWire[wireToolKey{kind: key.Kind(), name: wire}] = key.Clone()
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
				table.byWire[leafIndex] = key.Clone()
			}
		}
		if wire == "" {
			for ordinal := uint32(0); ; ordinal++ {
				wire = toolname.Alias(key.String(), key.Name(), ordinal)
				index := wireToolKey{kind: key.Kind(), name: wire}
				if _, occupied := table.byWire[index]; !occupied {
					table.byWire[index] = key.Clone()
					break
				}
				if ordinal == ^uint32(0) {
					return canonical.CanonicalRequest{}, ToolProjectionTable{}, decisions, fmt.Errorf("attempt tool projection alias space exhausted")
				}
			}
		}
		table.byCanonical[key] = wire
	}
	declared := map[canonical.ToolKey]bool{}
	for _, declaration := range request.Tools() {
		declared[declaration.Key()] = true
		if wire, _ := table.WireName(declaration.Key()); wire != declaration.Key().Name() {
			decisions = append(decisions, compat.Decision{Feature: compat.RequestToolsName, Outcome: compat.Approx, Subject: compat.Subject(declaration.Key().String())})
		}
	}
	for _, item := range request.Items() {
		if call, ok := item.ToolCall(); ok && !declared[call.Tool()] {
			if wire, _ := table.WireName(call.Tool()); wire != call.Tool().Name() {
				decisions = append(decisions, compat.Decision{Feature: compat.RequestItemsToolCallName, Outcome: compat.Approx, Subject: compat.Subject(call.Tool().String())})
			}
		}
	}
	projected, err := rewriteAttemptToolKeys(request, table)
	return projected, table, decisions, err
}

func rewriteAttemptToolKeys(request canonical.CanonicalRequest, table ToolProjectionTable) (canonical.CanonicalRequest, error) {
	declarations := request.Tools()
	projectedDeclarations := make([]canonical.ToolDeclaration, len(declarations))
	for i, declaration := range declarations {
		wire, _ := table.WireName(declaration.Key())
		key, err := canonical.NewToolKey(canonical.ToolNamespaceRequest, declaration.Kind(), wire)
		if err != nil {
			return canonical.CanonicalRequest{}, err
		}
		if function, ok := declaration.Function(); ok {
			projectedDeclarations[i], err = canonical.NewFunctionTool(key, function.Description(), function.InputSchema(), function.Strict())
		} else if custom, ok := declaration.Custom(); ok {
			projectedDeclarations[i], err = canonical.NewCustomTool(key, custom.Description(), custom.Format())
		}
		if err != nil {
			return canonical.CanonicalRequest{}, err
		}
	}
	toolSet, err := canonical.NewToolSet(projectedDeclarations)
	if err != nil {
		return canonical.CanonicalRequest{}, err
	}
	items := request.Items()
	for i, item := range items {
		call, ok := item.ToolCall()
		if !ok {
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
	params := canonical.RequestParams{Model: request.ModelField(), Instructions: request.InstructionsField(), Items: items, ToolPolicy: toolPolicy, ToolCallBatch: request.ToolCallBatchField(), Controls: request.Controls(), OutputFormat: request.OutputFormatField()}
	if request.ToolsSpecified() {
		params.Tools = canonical.Specify(toolSet)
	}
	if previous, ok := request.PreviousResponse(); ok {
		params.PreviousResponse = &previous
	}
	return canonical.NewCanonicalRequest(params), nil
}
