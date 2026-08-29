package responses

import (
	"encoding/json"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
	sse "github.com/swobuforge/swobu/internal/wire/framing/sse"
)

// ProviderRequestTool is one typed Responses tool declaration before exact-
// provider spelling and the single JSON serialization boundary.
type ProviderRequestTool struct {
	Type         string                `json:"type"`
	Name         string                `json:"name,omitempty"`
	Description  string                `json:"description,omitempty"`
	Parameters   any                   `json:"parameters,omitempty"`
	Strict       *bool                 `json:"strict,omitempty"`
	Format       any                   `json:"format,omitempty"`
	Tools        []ProviderRequestTool `json:"tools,omitempty"`
	Execution    string                `json:"execution,omitempty"`
	DeferLoading *bool                 `json:"defer_loading,omitempty"`
}

// ToolLoweringContext identifies one declaration during ordered lowering.
type ToolLoweringContext struct {
	Ordinal uint32
	Names   wire.ToolNames
}

// ToolLoweringRule replaces one semantic occurrence with zero or more target fragments.
type ToolLoweringRule func(ToolLoweringContext, canonical.ToolDeclaration) (fragments []ProviderRequestTool, handled bool, changes []compat.Change, err error)

// ToolPolicyLoweringRule resolves target policy after tool lowering.
type ToolPolicyLoweringRule func(canonical.ToolPolicy, wire.LoweredToolSet, wire.ToolNames) (choice any, handled bool, changes []compat.Change, err error)

type responsesToolProjection struct {
	tools   []ProviderRequestTool
	lowered wire.LoweredToolSet
	emitted map[canonical.ToolKey][]ProviderRequestTool
}

func (p responsesToolProjection) fragmentsFor(tools []canonical.ToolDeclaration) []ProviderRequestTool {
	var fragments []ProviderRequestTool
	for _, tool := range tools {
		if namespace, ok := tool.Namespace(); ok {
			fragments = append(fragments, p.fragmentsFor(namespace.Tools())...)
			continue
		}
		fragments = append(fragments, p.emitted[tool.Key()]...)
	}
	return fragments
}

func (p responsesToolProjection) emittedFor(key canonical.ToolKey) []ProviderRequestTool {
	if fragments, ok := p.emitted[key]; ok {
		return append([]ProviderRequestTool(nil), fragments...)
	}
	return nil
}

func encodeResponsesTools(tools []canonical.ToolDeclaration, visibility canonical.ToolVisibilityRefinements, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string) ([]ProviderRequestTool, error) {
	projection, err := compileResponsesToolProjection(tools, visibility, names, changeLog, exchangeID, nil)
	return projection.tools, err
}

func compileResponsesTools(tools []canonical.ToolDeclaration, visibility canonical.ToolVisibilityRefinements, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string, rule ToolLoweringRule) ([]ProviderRequestTool, wire.LoweredToolSet, error) {
	projection, err := compileResponsesToolProjection(tools, visibility, names, changeLog, exchangeID, rule)
	return projection.tools, projection.lowered, err
}

func compileResponsesToolProjection(tools []canonical.ToolDeclaration, visibility canonical.ToolVisibilityRefinements, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string, rule ToolLoweringRule) (responsesToolProjection, error) {
	if len(tools) == 0 {
		return responsesToolProjection{emitted: make(map[canonical.ToolKey][]ProviderRequestTool)}, nil
	}
	flattened, err := wire.PrepareFlatToolSet(tools, func(tool canonical.ToolDeclaration) (string, error) {
		return responsesFlatToolIdentity(tool, names)
	})
	if err != nil {
		return responsesToolProjection{}, err
	}
	if flattened.RemovedNamespaces > 0 {
		if err := appendResponsesRequestChange(changeLog, exchangeID, canonical.RequestTools, compat.Approximation); err != nil {
			return responsesToolProjection{}, err
		}
	}
	tools = flattened.Declarations
	out := make([]ProviderRequestTool, 0, len(tools))
	lowered := wire.LoweredToolSet{Records: make([]wire.LoweredToolRecord, 0, len(tools))}
	emitted := make(map[canonical.ToolKey][]ProviderRequestTool, len(tools))
	for ordinal, tool := range tools {
		if rule != nil {
			fragments, handled, changes, err := rule(ToolLoweringContext{Ordinal: uint32(ordinal), Names: names}, tool)
			if changeLog != nil {
				*changeLog = append(*changeLog, changes...)
			}
			if err != nil {
				return responsesToolProjection{}, err
			}
			if handled {
				out = append(out, fragments...)
				emitted[tool.Key()] = append([]ProviderRequestTool(nil), fragments...)
				lowered.Records = append(lowered.Records, wire.LoweredToolRecord{
					Key:           tool.Key(),
					Kind:          tool.Kind(),
					FragmentCount: len(fragments),
				})
				continue
			}
		}
		_, function := tool.Function()
		_, custom := tool.Custom()
		_, discovery := tool.Discovery()
		if !function && !custom && !discovery {
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key())))
			}
			lowered.Records = append(lowered.Records, wire.LoweredToolRecord{Key: tool.Key(), Kind: tool.Kind()})
			continue
		}
		wireTool, err := encodeResponsesTool(tool, names)
		if err != nil {
			return responsesToolProjection{}, err
		}
		if visibility.Deferred(tool.Key()) {
			deferred := true
			wireTool.DeferLoading = &deferred
		}
		out = append(out, wireTool)
		emitted[tool.Key()] = []ProviderRequestTool{wireTool}
		lowered.Records = append(lowered.Records, wire.LoweredToolRecord{
			Key:           tool.Key(),
			Kind:          tool.Kind(),
			FragmentCount: 1,
		})
	}
	return responsesToolProjection{tools: out, lowered: lowered, emitted: emitted}, nil
}

func responsesFlatToolIdentity(tool canonical.ToolDeclaration, names wire.ToolNames) (string, error) {
	switch tool.Kind() {
	case canonical.ToolKindFunction, canonical.ToolKindCustom:
		name, err := responsesToolWireName(tool, names)
		return string(tool.Kind()) + "\x00" + strings.TrimSpace(name), err
	case canonical.ToolKindWebSearch, canonical.ToolKindDiscovery:
		return string(tool.Kind()), nil
	default:
		return "", canonical.InternalError("Responses flat-tool identity received an unsupported declaration kind")
	}
}

func encodeResponsesTool(tool canonical.ToolDeclaration, names wire.ToolNames) (ProviderRequestTool, error) {
	if tool.Kind() == "" {
		return ProviderRequestTool{}, canonical.BadRequest("response request tool declarations are invalid")
	}
	if decl, ok := tool.Function(); ok {
		return encodeResponsesFunctionTool(tool, decl, names)
	}
	if decl, ok := tool.Custom(); ok {
		return encodeResponsesCustomTool(tool, decl, names)
	}
	if tool.Kind() == canonical.ToolKindWebSearch {
		return ProviderRequestTool{}, canonical.InternalError("Responses generic tool encoder received hosted web search after projection")
	}
	if discovery, ok := tool.Discovery(); ok {
		parameters, err := responsesToolParametersFromSchema(discovery.InputSchema())
		if err != nil {
			return ProviderRequestTool{}, err
		}
		execution := "server"
		if discovery.Executor() == canonical.DiscoveryExecutorClient {
			execution = "client"
		}
		return ProviderRequestTool{Type: "tool_search", Description: discovery.Description(), Parameters: parameters, Execution: execution}, nil
	}
	return ProviderRequestTool{}, canonical.InternalError("Responses tool encoder received an unsupported declaration kind")
}

func responsesNamespaceLeafName(name string) string {
	if index := strings.LastIndex(name, "/"); index >= 0 {
		return name[index+1:]
	}
	return name
}

func responsesClientNamespace(tool canonical.ToolKey) string {
	return responsesClientNamespaceValue(tool.Namespace())
}

func responsesClientNamespaceValue(namespace string) string {
	if namespace == canonical.ToolNamespaceRequest {
		return ""
	}
	return responsesNamespaceLeafName(namespace)
}

func resolveResponsesFunctionCall(tools []canonical.ToolDeclaration, namespace, name string) (canonical.ToolDeclaration, error) {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" {
		declaration, _, err := canonical.ResolveToolDeclarationByName(tools, name, canonical.ToolTypeFunction)
		return declaration, err
	}
	for _, tool := range tools {
		group, ok := tool.Namespace()
		if !ok || responsesNamespaceLeafName(group.Key().Name()) != namespace {
			continue
		}
		declaration, _, err := canonical.ResolveToolDeclarationByName(group.Tools(), name, canonical.ToolTypeFunction)
		return declaration, err
	}
	return canonical.ToolDeclaration{}, canonical.BadRequest("Responses function call references an unknown namespace")
}

// resolveHistoricalResponsesFunctionCall preserves a client transcript call by
// value when its scoped declaration is no longer part of the current request.
// Provider output still uses strict attempt-local declaration resolution.
func resolveHistoricalResponsesFunctionCall(tools []canonical.ToolDeclaration, namespace, name string) (canonical.ToolKey, error) {
	if strings.TrimSpace(namespace) == "" {
		return canonical.ResolveHistoricalToolKeyByName(tools, name, canonical.ToolKindFunction)
	}
	historical, err := canonical.HistoricalScopedToolKey(namespace, name, canonical.ToolKindFunction)
	if err != nil {
		return canonical.ToolKey{}, err
	}
	if declaration, err := resolveResponsesFunctionCall(tools, namespace, name); err == nil {
		return declaration.Key(), nil
	}
	return historical, nil
}

func encodeResponsesFunctionTool(declaration canonical.ToolDeclaration, decl canonical.FunctionTool, names wire.ToolNames) (ProviderRequestTool, error) {
	name, err := responsesToolWireName(declaration, names)
	if err != nil {
		return ProviderRequestTool{}, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return ProviderRequestTool{}, canonical.BadRequest("response request tool declarations require a name")
	}
	parameters, err := responsesToolParametersFromSchema(decl.InputSchema())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	wire := ProviderRequestTool{
		Type:        "function",
		Name:        name,
		Description: strings.TrimSpace(decl.Description()), // swobu:io-string source=boundary
		Parameters:  parameters,
	}
	if strict, ok := decl.Strict().Get(); ok {
		wire.Strict = &strict
	}
	return wire, nil
}

func encodeResponsesCustomTool(declaration canonical.ToolDeclaration, decl canonical.CustomTool, names wire.ToolNames) (ProviderRequestTool, error) {
	name, err := responsesToolWireName(declaration, names)
	if err != nil {
		return ProviderRequestTool{}, err
	}
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return ProviderRequestTool{}, canonical.BadRequest("response request custom tool declarations require a name")
	}
	format, err := responsesToolFormatFromCanonical(decl.Format())
	if err != nil {
		return ProviderRequestTool{}, err
	}
	return ProviderRequestTool{
		Type:        "custom",
		Name:        name,
		Description: strings.TrimSpace(decl.Description()), // swobu:io-string source=boundary
		Format:      format,
	}, nil
}

func responsesToolParametersFromSchema(schema canonical.ToolSchema) (any, error) {
	raw := strings.TrimSpace(schema.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, canonical.BadRequest("response request tool declarations require input_schema")
	}
	if _, err := sse.DecodeJSONObject(json.RawMessage(raw), "response request tool declaration input_schema is invalid"); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func responsesToolFormatFromCanonical(format canonical.ToolFormat) (any, error) {
	raw := strings.TrimSpace(format.RawObject()) // swobu:io-string source=domain
	if raw == "" {
		return nil, canonical.BadRequest("response request tool declarations require format")
	}
	if _, err := sse.DecodeJSONObject(json.RawMessage(raw), "response request tool declaration format is invalid"); err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func encodeToolChoice(policy canonical.ToolPolicy, projection responsesToolProjection, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string) (any, error) {
	lowered := projection.lowered
	if lowered.TotalFragments() == 0 {
		if policy.Mode == canonical.ToolPolicyRequired || policy.Mode == canonical.ToolPolicySpecific {
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolPolicy, canonical.Occurrence{}))
			}
		}
		return nil, nil
	}
	switch policy.Mode {
	case canonical.ToolPolicyNone:
		return "none", nil
	case canonical.ToolPolicyAuto:
		return "auto", nil
	case canonical.ToolPolicyRequired:
		return "required", nil
	case canonical.ToolPolicySpecific:
		key, ok := policy.SpecificID()
		if !ok {
			return nil, canonical.BadRequest("specific tool policy requires a tool id")
		}
		fragments := projection.emitted[key]
		if len(fragments) != 1 {
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolPolicy, canonical.Occurrence{}))
			}
			return nil, nil
		}
		fragment := fragments[0]
		switch fragment.Type {
		case "function", "custom":
			return map[string]any{"type": fragment.Type, "name": strings.TrimSpace(fragment.Name)}, nil
		case "tool_search":
			return map[string]any{"type": fragment.Type}, nil
		default:
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolPolicy, canonical.Occurrence{}))
			}
			return nil, nil
		}
	default:
		return nil, canonical.BadRequest("response request tool_choice is invalid")
	}
}

type responsesToolNamespaceContext struct {
	path          []string
	subjectPrefix string
	index         int
}

func cloneBoolPointer(ptr *bool) *bool {
	if ptr == nil {
		return nil
	}
	cloned := *ptr
	return &cloned
}
