package canonical

import "strings"

const (
	ToolTypeFunction = "function"
	ToolTypeCustom   = "custom"
)

// ToolOrigin identifies the semantic owner of one tool.
type ToolOrigin string

const (
	ToolOriginRequest ToolOrigin = "request"
)

// ToolKind identifies the semantic kind of one tool.
type ToolKind string

const (
	ToolKindFunction   ToolKind = "function"
	ToolKindCustom     ToolKind = "custom"
	ToolKindCapability ToolKind = "capability"
)

// SemanticToolID is the canonical identifier for one semantic tool
// declaration or tool-call target.
//
// It is structured as origin + kind + path and renders as
// tool:v1/{origin}/{kind}/{path}.
type SemanticToolID struct {
	Origin ToolOrigin
	Kind   ToolKind
	Path   string
}

// NewSemanticToolID normalizes one semantic tool identifier into canonical
// form.
func NewSemanticToolID(raw string) SemanticToolID {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=domain
	if trimmed == "" {
		return SemanticToolID{}
	}
	if parsed, ok := parseCanonicalSemanticToolID(trimmed); ok {
		return parsed
	}
	return SemanticToolID{Path: trimmed}
}

func NewSemanticToolIDFor(origin ToolOrigin, kind ToolKind, raw string) SemanticToolID {
	id := NewSemanticToolID(raw)
	if strings.TrimSpace(string(id.Origin)) == "" { // swobu:io-string source=domain
		id.Origin = origin
	}
	if strings.TrimSpace(string(id.Kind)) == "" { // swobu:io-string source=domain
		id.Kind = kind
	}
	if strings.TrimSpace(id.Path) == "" { // swobu:io-string source=domain
		id.Path = strings.TrimSpace(raw) // swobu:io-string source=domain
	}
	return id
}

func parseCanonicalSemanticToolID(raw string) (SemanticToolID, bool) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=domain
	const prefix = "tool:v1/"
	if !strings.HasPrefix(trimmed, prefix) {
		return SemanticToolID{}, false
	}
	rest := strings.TrimPrefix(trimmed, prefix)
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) != 3 {
		return SemanticToolID{}, false
	}
	origin := strings.TrimSpace(parts[0]) // swobu:io-string source=domain
	kind := strings.TrimSpace(parts[1])   // swobu:io-string source=domain
	path := strings.TrimSpace(parts[2])   // swobu:io-string source=domain
	if origin == "" || kind == "" || path == "" {
		return SemanticToolID{}, false
	}
	return SemanticToolID{
		Origin: ToolOrigin(origin),
		Kind:   ToolKind(kind),
		Path:   path,
	}, true
}

func (id SemanticToolID) IsZero() bool {
	return strings.TrimSpace(string(id.Origin)) == "" && // swobu:io-string source=domain
		strings.TrimSpace(string(id.Kind)) == "" && // swobu:io-string source=domain
		strings.TrimSpace(id.Path) == "" // swobu:io-string source=domain
}

func (id SemanticToolID) String() string {
	origin := strings.TrimSpace(string(id.Origin)) // swobu:io-string source=domain
	kind := strings.TrimSpace(string(id.Kind))     // swobu:io-string source=domain
	path := strings.TrimSpace(id.Path)             // swobu:io-string source=domain
	if origin != "" && kind != "" && path != "" {
		return "tool:v1/" + origin + "/" + kind + "/" + path
	}
	if path != "" {
		return path
	}
	return ""
}

func (id SemanticToolID) Clone() SemanticToolID {
	return SemanticToolID{
		Origin: id.Origin,
		Kind:   id.Kind,
		Path:   id.Path,
	}
}

// ToolExecutionOwner identifies which runtime owns tool execution.
type ToolExecutionOwner string

// ToolExecutionOwner enumerates the supported execution ownership classes for
// semantic tools.
const (
	ToolOwnerClient   ToolExecutionOwner = "client"
	ToolOwnerProvider ToolExecutionOwner = "provider"
	ToolOwnerSwobu    ToolExecutionOwner = "swobu"
	ToolOwnerExternal ToolExecutionOwner = "external"
)

func normalizeToolExecutionOwner(owner ToolExecutionOwner) ToolExecutionOwner {
	switch owner {
	case ToolOwnerClient, ToolOwnerProvider, ToolOwnerSwobu, ToolOwnerExternal:
		return owner
	default:
		return ToolOwnerClient
	}
}

// ToolCapability names a provider-independent semantic capability.
type ToolCapability string

// NewToolCapability normalizes one semantic capability name into canonical
// form.
func NewToolCapability(raw string) ToolCapability {
	return ToolCapability(strings.TrimSpace(raw)) // swobu:io-string source=domain
}

func (c ToolCapability) IsZero() bool {
	return strings.TrimSpace(string(c)) == "" // swobu:io-string source=domain
}

func (c ToolCapability) String() string {
	return string(c)
}

// ToolCapabilityConfig stores provider-independent semantic configuration for a
// capability-style tool as one JSON object payload.
type ToolCapabilityConfig struct {
	rawObject string
}

// EmptyToolCapabilityConfig returns an empty capability configuration.
func EmptyToolCapabilityConfig() ToolCapabilityConfig {
	return ToolCapabilityConfig{}
}

// NewToolCapabilityConfigObject normalizes one capability configuration JSON
// object into canonical form.
func NewToolCapabilityConfigObject(raw string) ToolCapabilityConfig {
	return ToolCapabilityConfig{rawObject: raw}
}

func (c ToolCapabilityConfig) RawObject() string {
	return c.rawObject
}

func (c ToolCapabilityConfig) IsEmpty() bool {
	return strings.TrimSpace(c.rawObject) == "" // swobu:io-string source=domain
}

// ToolFormat stores semantic custom-tool formatting as one JSON object
// payload.
type ToolFormat struct {
	rawObject string
}

// EmptyToolFormat returns an empty custom-tool format.
func EmptyToolFormat() ToolFormat {
	return ToolFormat{}
}

// NewToolFormatObject normalizes one custom-tool format JSON object into
// canonical form.
func NewToolFormatObject(raw string) ToolFormat {
	return ToolFormat{rawObject: raw}
}

func (f ToolFormat) RawObject() string {
	return f.rawObject
}

func (f ToolFormat) IsEmpty() bool {
	return strings.TrimSpace(f.rawObject) == "" // swobu:io-string source=domain
}

// ToolDecl preserves the semantic request-side tool declaration surface.
//
// FunctionToolDecl is the common request-path shape. CapabilityToolDecl is
// reserved for provider-independent facts that Swobu understands
// semantically but may not be able to lower to every wire family.
type ToolDecl interface {
	toolDecl()
	ToolID() SemanticToolID
	Owner() ToolExecutionOwner
	Clone() ToolDecl
	ToolName() string
	ToolDescription() string
	ToolInputSchema() ToolSchema
	ToolCapability() ToolCapability
	CapabilityConfig() ToolCapabilityConfig
}

// ToolDeclKind reports the wire-visible tool kind for one semantic tool
// declaration.
func ToolDeclKind(tool ToolDecl) string {
	switch decl := tool.(type) {
	case FunctionToolDecl:
		return ToolTypeFunction
	case *FunctionToolDecl:
		return ToolTypeFunction
	case CustomToolDecl:
		return ToolTypeCustom
	case *CustomToolDecl:
		return ToolTypeCustom
	case CapabilityToolDecl:
		return strings.ToLower(strings.TrimSpace(string(decl.ToolCapability()))) // swobu:io-string source=domain
	case *CapabilityToolDecl:
		return strings.ToLower(strings.TrimSpace(string(decl.ToolCapability()))) // swobu:io-string source=domain
	default:
		return ""
	}
}

func normalizeSemanticToolID(id SemanticToolID, origin ToolOrigin, kind ToolKind, fallbackPath string) SemanticToolID {
	normalized := id.Clone()
	if strings.TrimSpace(string(normalized.Origin)) == "" { // swobu:io-string source=domain
		normalized.Origin = origin
	}
	if strings.TrimSpace(string(normalized.Kind)) == "" { // swobu:io-string source=domain
		normalized.Kind = kind
	}
	if strings.TrimSpace(normalized.Path) == "" { // swobu:io-string source=domain
		normalized.Path = strings.TrimSpace(fallbackPath) // swobu:io-string source=domain
	}
	return normalized
}

// ResolveToolDeclByID finds one uniquely identified tool declaration in a flat
// tool surface.
func ResolveToolDeclByID(tools []ToolDecl, id SemanticToolID, specificType string) (ToolDecl, string, error) {
	if id.IsZero() {
		return nil, "", BadRequest("canonical request tool references require a tool id")
	}
	normalizedSpecific := strings.ToLower(strings.TrimSpace(specificType)) // swobu:io-string source=domain
	var (
		found     ToolDecl
		foundType string
		matched   bool
	)
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		if tool.ToolID() != id {
			continue
		}
		toolType := ToolDeclKind(tool)
		if normalizedSpecific != "" && toolType != normalizedSpecific {
			continue
		}
		if matched && (found.ToolID() != tool.ToolID() || foundType != toolType) {
			return nil, "", BadRequest("canonical request tool references are ambiguous")
		}
		found = tool
		foundType = toolType
		matched = true
	}
	if !matched {
		return nil, "", BadRequest("canonical request tool references an undeclared tool")
	}
	return found, foundType, nil
}

// ResolveToolDeclByName resolves one projected wire name against a flat tool
// surface.
//
// Flat-name protocols can only lower tool intent when the projected name is
// provably unique. If the name is missing or ambiguous, the projection must
// reject the request.
func ResolveToolDeclByName(tools []ToolDecl, name string, specificType string) (ToolDecl, string, error) {
	trimmed := strings.TrimSpace(name) // swobu:io-string source=domain
	if trimmed == "" {
		return nil, "", BadRequest("canonical request tool references require a name")
	}
	normalizedSpecific := strings.ToLower(strings.TrimSpace(specificType)) // swobu:io-string source=domain
	if normalizedSpecific != "" {
		kind := ToolKind(normalizedSpecific)
		if specificID, _, projected := ToolIdentityFromWire(trimmed, kind); projected {
			if found, foundType, resolveErr := ResolveToolDeclByID(tools, specificID, normalizedSpecific); resolveErr == nil {
				return found, foundType, nil
			}
		}
		return resolvePlainToolDeclByName(tools, trimmed, normalizedSpecific)
	}
	if specificID, _, projected := ToolIdentityFromWire(trimmed, ToolKindFunction); projected {
		if found, foundType, resolveErr := ResolveToolDeclByID(tools, specificID, ToolTypeFunction); resolveErr == nil {
			return found, foundType, nil
		}
	}
	if specificID, _, projected := ToolIdentityFromWire(trimmed, ToolKindCustom); projected {
		if found, foundType, resolveErr := ResolveToolDeclByID(tools, specificID, ToolTypeCustom); resolveErr == nil {
			return found, foundType, nil
		}
	}
	return resolvePlainToolDeclByName(tools, trimmed, "")
}

func resolvePlainToolDeclByName(tools []ToolDecl, name string, specificType string) (ToolDecl, string, error) {
	normalizedSpecific := strings.ToLower(strings.TrimSpace(specificType)) // swobu:io-string source=domain
	var (
		found     ToolDecl
		foundType string
		matched   bool
	)
	for _, tool := range tools {
		if tool == nil {
			continue
		}
		toolType := ToolDeclKind(tool)
		if normalizedSpecific != "" && toolType != normalizedSpecific {
			continue
		}
		if strings.Contains(strings.TrimSpace(tool.ToolID().Path), "/") { // swobu:io-string source=domain
			continue
		}
		if strings.TrimSpace(tool.ToolName()) != name { // swobu:io-string source=domain
			continue
		}
		if matched && (found.ToolID() != tool.ToolID() || foundType != toolType) {
			return nil, "", BadRequest("canonical request tool references are ambiguous")
		}
		found = tool
		foundType = toolType
		matched = true
	}
	if !matched {
		return nil, "", BadRequest("canonical request tool references are undeclared tool")
	}
	return found, foundType, nil
}

func cloneBoolPointer(ptr *bool) *bool {
	if ptr == nil {
		return nil
	}
	cloned := *ptr
	return &cloned
}
