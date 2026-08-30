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
	CallType     string                `json:"-"`
	ResultType   string                `json:"-"`
}

// ToolLoweringContext identifies one declaration during ordered lowering.
type ToolLoweringContext struct {
	Ordinal uint32
	Names   wire.ToolNames
}

// ToolProjection is the complete Responses manifestation selected for one
// canonical declaration occurrence.
type ToolProjection struct {
	Fragments                      []ProviderRequestTool
	TargetType                     string
	TargetName                     string
	SupportsWebSearchSourceInclude bool
	ProjectCall                    func(canonical.ToolCallItem) (toolCallProjection, error)
	ProjectResult                  func(canonical.ToolResultItem) (toolResultProjection, error)
}

// toolCallProjection is a closed Responses-local call union. Implementations
// are wire DTOs or typed compiler values; shared provenance never carries it.
type toolCallProjection interface{ responsesToolCallProjection() }

type toolResultProjection struct{ Type string }

// ToolTransformer totally projects one semantic tool slot occurrence.
type ToolTransformer func(ToolLoweringContext, canonical.ToolDeclaration) (ToolProjection, []compat.Change, error)

// ToolLowering is the resolved Responses tool algebra. Every slot is total
// before request encoding begins.
type ToolLowering struct {
	Function  ToolTransformer
	Custom    ToolTransformer
	WebSearch ToolTransformer
	Discovery ToolTransformer
}

// Overlay replaces only explicitly supplied slots.
func (l ToolLowering) Overlay(override ToolLowering) ToolLowering {
	if override.Function != nil {
		l.Function = override.Function
	}
	if override.Custom != nil {
		l.Custom = override.Custom
	}
	if override.WebSearch != nil {
		l.WebSearch = override.WebSearch
	}
	if override.Discovery != nil {
		l.Discovery = override.Discovery
	}
	return l
}

// ProtocolToolLowering returns the total Responses protocol baseline.
// Provider construction overlays only proven divergences onto this value.
func ProtocolToolLowering() ToolLowering {
	return DefaultToolLowering()
}

// DefaultToolLowering returns official Responses protocol semantics.
func DefaultToolLowering() ToolLowering {
	nativeFunction := func(ctx ToolLoweringContext, tool canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
		encoded, err := encodeResponsesTool(tool, ctx.Names)
		if err != nil {
			return ToolProjection{}, nil, err
		}
		return functionToolProjection(encoded), nil, nil
	}
	nativeCustom := func(ctx ToolLoweringContext, tool canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
		encoded, err := encodeResponsesTool(tool, ctx.Names)
		if err != nil {
			return ToolProjection{}, nil, err
		}
		return customToolProjection(encoded), nil, nil
	}
	return ToolLowering{
		Function: nativeFunction,
		Custom:   nativeCustom,
		WebSearch: func(_ ToolLoweringContext, tool canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
			return webSearchToolProjection(), []compat.Change{compat.NewOmission(canonical.RequestToolsKind, canonical.ToolOccurrence(tool.Key()))}, nil
		},
		Discovery: func(ctx ToolLoweringContext, tool canonical.ToolDeclaration) (ToolProjection, []compat.Change, error) {
			encoded, err := encodeResponsesTool(tool, ctx.Names)
			if err != nil {
				return ToolProjection{}, nil, err
			}
			return discoveryToolProjection(encoded), nil, nil
		},
	}
}

func functionToolProjection(encoded ProviderRequestTool) ToolProjection {
	return ToolProjection{
		Fragments: []ProviderRequestTool{encoded}, TargetType: encoded.Type, TargetName: encoded.Name,
		ProjectCall: func(call canonical.ToolCallItem) (toolCallProjection, error) {
			object, ok := call.Input().Object()
			if !ok {
				return nil, canonical.BadRequest("responses function projection requires object input")
			}
			return functionCallItem{Type: "function_call", CallID: call.CallID().String(), Name: encoded.Name, Arguments: object.String()}, nil
		},
		ProjectResult: func(canonical.ToolResultItem) (toolResultProjection, error) {
			return toolResultProjection{Type: "function_call_output"}, nil
		},
	}
}

func customToolProjection(encoded ProviderRequestTool) ToolProjection {
	return ToolProjection{
		Fragments: []ProviderRequestTool{encoded}, TargetType: encoded.Type, TargetName: encoded.Name,
		ProjectCall: func(call canonical.ToolCallItem) (toolCallProjection, error) {
			text, ok := call.Input().Text()
			if !ok {
				return nil, canonical.BadRequest("responses custom projection requires text input")
			}
			return customToolCallItem{Type: "custom_tool_call", CallID: call.CallID().String(), Name: encoded.Name, Input: text}, nil
		},
		ProjectResult: func(canonical.ToolResultItem) (toolResultProjection, error) {
			return toolResultProjection{Type: "custom_tool_call_output"}, nil
		},
	}
}

// CustomAsFunctionProjection returns a complete Custom occurrence projection
// using one provider-selected Function argument property.
func CustomAsFunctionProjection(encoded ProviderRequestTool, argumentName string) ToolProjection {
	return ToolProjection{
		Fragments: []ProviderRequestTool{encoded}, TargetType: encoded.Type, TargetName: encoded.Name,
		ProjectCall: func(call canonical.ToolCallItem) (toolCallProjection, error) {
			text, ok := call.Input().Text()
			if !ok {
				return nil, canonical.BadRequest("responses Custom function projection requires text input")
			}
			arguments, err := json.Marshal(map[string]string{argumentName: text})
			if err != nil {
				return nil, canonical.InternalError("Responses Custom function arguments could not be encoded")
			}
			return functionCallItem{Type: "function_call", CallID: call.CallID().String(), Name: encoded.Name, Arguments: string(arguments)}, nil
		},
		ProjectResult: func(canonical.ToolResultItem) (toolResultProjection, error) {
			return toolResultProjection{Type: "function_call_output"}, nil
		},
	}
}

func webSearchToolProjection() ToolProjection {
	return ToolProjection{
		ProjectCall: func(call canonical.ToolCallItem) (toolCallProjection, error) {
			search, ok := call.Input().WebSearch()
			if !ok {
				return nil, canonical.BadRequest("responses web-search calls require typed input")
			}
			action, err := encodeResponsesWebSearchAction(search)
			if err != nil {
				return nil, canonical.BadRequest("responses web-search action could not be encoded")
			}
			item := responsesWireOutputItemDTO{Type: "web_search_call", Status: "in_progress", Action: action}
			if refinement, ok := call.ResponsesWebSearch(); ok {
				item.ID = refinement.ItemID().String()
			}
			return webSearchCallProjection{item: item}, nil
		},
		ProjectResult: func(canonical.ToolResultItem) (toolResultProjection, error) {
			return toolResultProjection{Type: "web_search_call"}, nil
		},
	}
}

// HostedSearchProjection returns the complete Responses hosted-search
// manifestation selected by a protocol or provider WebSearch slot.
func HostedSearchProjection(fragment ProviderRequestTool, supportsSourceInclude bool) ToolProjection {
	projection := webSearchToolProjection()
	projection.Fragments = []ProviderRequestTool{fragment}
	projection.TargetType = fragment.Type
	projection.TargetName = fragment.Name
	projection.SupportsWebSearchSourceInclude = supportsSourceInclude
	return projection
}

func discoveryToolProjection(encoded ProviderRequestTool) ToolProjection {
	return ToolProjection{
		Fragments: []ProviderRequestTool{encoded}, TargetType: encoded.Type, TargetName: encoded.Name,
		ProjectCall: func(call canonical.ToolCallItem) (toolCallProjection, error) {
			object, ok := call.Input().Object()
			if !ok {
				return nil, canonical.BadRequest("responses tool discovery calls require object input")
			}
			executor, ok := call.DiscoveryExecutor()
			if !ok {
				return nil, canonical.InternalError("responses tool discovery call lost execution ownership")
			}
			execution := "client"
			if executor == canonical.DiscoveryExecutorProvider {
				execution = "server"
			}
			var callID any = call.CallID().String()
			if call.ResponsesCallIDNull() {
				callID = nil
			}
			return toolSearchCallProjection{callID: callID, execution: execution, arguments: json.RawMessage(object.String())}, nil
		},
		ProjectResult: func(canonical.ToolResultItem) (toolResultProjection, error) {
			return toolResultProjection{Type: "tool_search_output"}, nil
		},
	}
}

type webSearchCallProjection struct {
	item responsesWireOutputItemDTO
}

type toolSearchCallProjection struct {
	callID    any
	execution string
	arguments json.RawMessage
}

func (functionCallItem) responsesToolCallProjection()         {}
func (customToolCallItem) responsesToolCallProjection()       {}
func (webSearchCallProjection) responsesToolCallProjection()  {}
func (toolSearchCallProjection) responsesToolCallProjection() {}

type responsesToolProjection struct {
	tools       []ProviderRequestTool
	lowered     wire.LoweredToolSet
	emitted     map[canonical.ToolKey][]ProviderRequestTool
	occurrences map[canonical.ToolKey]ToolProjection
	lowering    ToolLowering
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
	projection, err := compileResponsesToolProjection(tools, visibility, names, changeLog, exchangeID, DefaultToolLowering())
	return projection.tools, err
}

func compileResponsesTools(tools []canonical.ToolDeclaration, visibility canonical.ToolVisibilityRefinements, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string, lowering ToolLowering) ([]ProviderRequestTool, wire.LoweredToolSet, error) {
	projection, err := compileResponsesToolProjection(tools, visibility, names, changeLog, exchangeID, lowering)
	return projection.tools, projection.lowered, err
}

func compileResponsesToolProjection(tools []canonical.ToolDeclaration, visibility canonical.ToolVisibilityRefinements, names wire.ToolNames, changeLog *[]compat.Change, exchangeID string, lowering ToolLowering) (responsesToolProjection, error) {
	if !lowering.resolved() {
		return responsesToolProjection{}, canonical.InternalError("Responses tool compilation requires resolved lowering")
	}
	if len(tools) == 0 {
		return responsesToolProjection{emitted: make(map[canonical.ToolKey][]ProviderRequestTool), occurrences: make(map[canonical.ToolKey]ToolProjection), lowering: lowering}, nil
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
	occurrences := make(map[canonical.ToolKey]ToolProjection, len(tools))
	for ordinal, tool := range tools {
		var transformer ToolTransformer
		switch tool.Kind() {
		case canonical.ToolKindFunction:
			transformer = lowering.Function
		case canonical.ToolKindCustom:
			transformer = lowering.Custom
		case canonical.ToolKindWebSearch:
			transformer = lowering.WebSearch
		case canonical.ToolKindDiscovery:
			transformer = lowering.Discovery
		default:
			return responsesToolProjection{}, canonical.InternalError("Responses lowering received unsupported tool kind")
		}
		projectedTool, changes, err := transformer(ToolLoweringContext{Ordinal: uint32(ordinal), Names: names}, tool)
		if changeLog != nil {
			*changeLog = append(*changeLog, changes...)
		}
		if err != nil {
			return responsesToolProjection{}, err
		}
		if visibility.Deferred(tool.Key()) {
			for index := range projectedTool.Fragments {
				deferred := true
				projectedTool.Fragments[index].DeferLoading = &deferred
			}
		}
		out = append(out, projectedTool.Fragments...)
		emitted[tool.Key()] = append([]ProviderRequestTool(nil), projectedTool.Fragments...)
		occurrences[tool.Key()] = projectedTool
		record := wire.LoweredToolRecord{
			Key: tool.Key(), Kind: tool.Kind(), FragmentCount: len(projectedTool.Fragments),
			TargetType: projectedTool.TargetType, TargetName: projectedTool.TargetName,
		}
		lowered.Records = append(lowered.Records, record)
	}
	return responsesToolProjection{tools: out, lowered: lowered, emitted: emitted, occurrences: occurrences, lowering: lowering}, nil
}

func (l ToolLowering) resolved() bool {
	return l.Function != nil && l.Custom != nil && l.WebSearch != nil && l.Discovery != nil
}

func (p responsesToolProjection) historicalProjection(call canonical.ToolCallItem, names wire.ToolNames) (ToolProjection, error) {
	key := call.Tool()
	if projection, ok := p.occurrences[key]; ok {
		return projection, nil
	}
	switch key.Kind() {
	case canonical.ToolKindFunction, canonical.ToolKindCustom:
		return ToolProjection{}, nil
	case canonical.ToolKindWebSearch:
		projection, _, err := p.lowering.WebSearch(ToolLoweringContext{Names: names}, canonical.NewWebSearchDeclaration())
		return projection, err
	case canonical.ToolKindDiscovery:
		// Discovery projection may depend on declaration schema, description, and
		// execution semantics. Without retained declaration provenance, omission
		// is the only honest result; never synthesize transformer input.
		return ToolProjection{}, nil
	default:
		return ToolProjection{}, canonical.InternalError("Responses history has unsupported canonical tool kind")
	}
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
		return ProviderRequestTool{Type: "tool_search", Description: discovery.Description(), Parameters: parameters, Execution: execution, CallType: "tool_search_call", ResultType: "tool_search_output"}, nil
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
		CallType:    "function_call",
		ResultType:  "function_call_output",
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
		CallType:    "custom_tool_call",
		ResultType:  "custom_tool_call_output",
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
		case "":
			if changeLog != nil {
				*changeLog = compat.AppendUnique(*changeLog, compat.NewOmission(canonical.RequestToolPolicy, canonical.Occurrence{}))
			}
			return nil, nil
		default:
			// Hosted tool spellings are the emitted target identity. Specific
			// policy consumes that identity without a second provider callback.
			return map[string]any{"type": fragment.Type}, nil
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
