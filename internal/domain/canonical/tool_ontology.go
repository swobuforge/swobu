package canonical

import "strings"

const (
	ToolTypeFunction = "function"
	ToolTypeCustom   = "custom"
)

// ToolOrigin identifies the semantic owner of one tool.
type ToolOrigin string

const (
	ToolOriginRequest  ToolOrigin = "request"
	ToolOriginProvider ToolOrigin = "provider"
	ToolOriginMCP      ToolOrigin = "mcp"
	ToolOriginSwobu    ToolOrigin = "swobu"
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
	if strings.TrimSpace(string(id.Origin)) == "" {
		id.Origin = origin
	}
	if strings.TrimSpace(string(id.Kind)) == "" {
		id.Kind = kind
	}
	if strings.TrimSpace(id.Path) == "" {
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
	return strings.TrimSpace(string(id.Origin)) == "" &&
		strings.TrimSpace(string(id.Kind)) == "" &&
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
// reserved for provider-independent capabilities that Swobu understands
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
		return strings.ToLower(strings.TrimSpace(string(decl.ToolCapability())))
	case *CapabilityToolDecl:
		return strings.ToLower(strings.TrimSpace(string(decl.ToolCapability())))
	default:
		return ""
	}
}

func normalizeSemanticToolID(id SemanticToolID, origin ToolOrigin, kind ToolKind, fallbackPath string) SemanticToolID {
	normalized := id.Clone()
	if strings.TrimSpace(string(normalized.Origin)) == "" {
		normalized.Origin = origin
	}
	if strings.TrimSpace(string(normalized.Kind)) == "" {
		normalized.Kind = kind
	}
	if strings.TrimSpace(normalized.Path) == "" {
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
		specificID, _, err := ParseProjectedToolName(trimmed, kind)
		if err != nil {
			return nil, "", err
		}
		return ResolveToolDeclByID(tools, specificID, normalizedSpecific)
	}
	if specificID, _, err := ParseProjectedToolName(trimmed, ToolKindFunction); err == nil {
		if found, foundType, resolveErr := ResolveToolDeclByID(tools, specificID, ToolTypeFunction); resolveErr == nil {
			return found, foundType, nil
		}
	}
	if specificID, _, err := ParseProjectedToolName(trimmed, ToolKindCustom); err == nil {
		if found, foundType, resolveErr := ResolveToolDeclByID(tools, specificID, ToolTypeCustom); resolveErr == nil {
			return found, foundType, nil
		}
	}
	return nil, "", BadRequest("canonical request tool references are undeclared")
}

// FunctionToolDecl represents an ordinary JSON-schema callable tool.
type FunctionToolDecl struct {
	ID          SemanticToolID
	Name        string
	Description string
	InputSchema ToolSchema
	Strict      *bool
	Execution   ToolExecutionOwner
}

// NewFunctionToolDecl normalizes one callable tool declaration into canonical
// form.
func NewFunctionToolDecl(id, name, description string, inputSchema ToolSchema) FunctionToolDecl {
	normalized := FunctionToolDecl{
		ID:          NewSemanticToolIDFor(ToolOriginRequest, ToolKindFunction, id),
		Name:        strings.TrimSpace(name),        // swobu:io-string source=domain
		Description: strings.TrimSpace(description), // swobu:io-string source=domain
		InputSchema: NewToolSchemaObject(inputSchema.RawObject()),
		Strict:      nil,
		Execution:   ToolOwnerClient,
	}
	if normalized.ID.IsZero() {
		normalized.ID = NewSemanticToolIDFor(ToolOriginRequest, ToolKindFunction, normalized.Name)
	}
	return normalized
}

func (d FunctionToolDecl) toolDecl() {}

func (d FunctionToolDecl) ToolID() SemanticToolID {
	return normalizeSemanticToolID(d.ID, ToolOriginRequest, ToolKindFunction, d.Name)
}

func (d FunctionToolDecl) Owner() ToolExecutionOwner {
	return normalizeToolExecutionOwner(d.Execution)
}

func (d FunctionToolDecl) Clone() ToolDecl {
	return FunctionToolDecl{
		ID:          d.ID.Clone(),
		Name:        d.Name,
		Description: d.Description,
		InputSchema: NewToolSchemaObject(d.InputSchema.RawObject()),
		Strict:      cloneBoolPointer(d.Strict),
		Execution:   normalizeToolExecutionOwner(d.Execution),
	}
}

func (d FunctionToolDecl) ToolName() string {
	return d.Name
}

func (d FunctionToolDecl) ToolDescription() string {
	return d.Description
}

func (d FunctionToolDecl) ToolInputSchema() ToolSchema {
	return NewToolSchemaObject(d.InputSchema.RawObject())
}

func (d FunctionToolDecl) ToolCapability() ToolCapability {
	return ""
}

func (d FunctionToolDecl) CapabilityConfig() ToolCapabilityConfig {
	return EmptyToolCapabilityConfig()
}

// CustomToolDecl represents a custom tool with a raw format payload that must
// survive protocol translation without flattening.
type CustomToolDecl struct {
	ID          SemanticToolID
	Name        string
	Description string
	Format      ToolFormat
	Execution   ToolExecutionOwner
}

// NewCustomToolDecl normalizes one custom tool declaration into canonical
// form.
func NewCustomToolDecl(id, name, description string, format ToolFormat) CustomToolDecl {
	normalized := CustomToolDecl{
		ID:          NewSemanticToolIDFor(ToolOriginRequest, ToolKindCustom, id),
		Name:        strings.TrimSpace(name),        // swobu:io-string source=domain
		Description: strings.TrimSpace(description), // swobu:io-string source=domain
		Format:      NewToolFormatObject(format.RawObject()),
		Execution:   ToolOwnerClient,
	}
	if normalized.ID.IsZero() {
		normalized.ID = NewSemanticToolIDFor(ToolOriginRequest, ToolKindCustom, normalized.Name)
	}
	return normalized
}

func (d CustomToolDecl) toolDecl() {}

func (d CustomToolDecl) ToolID() SemanticToolID {
	return normalizeSemanticToolID(d.ID, ToolOriginRequest, ToolKindCustom, d.Name)
}

func (d CustomToolDecl) Owner() ToolExecutionOwner {
	return normalizeToolExecutionOwner(d.Execution)
}

func (d CustomToolDecl) Clone() ToolDecl {
	return CustomToolDecl{
		ID:          d.ID.Clone(),
		Name:        d.Name,
		Description: d.Description,
		Format:      NewToolFormatObject(d.Format.RawObject()),
		Execution:   normalizeToolExecutionOwner(d.Execution),
	}
}

func (d CustomToolDecl) ToolName() string {
	return d.Name
}

func (d CustomToolDecl) ToolDescription() string {
	return d.Description
}

func (d CustomToolDecl) ToolInputSchema() ToolSchema {
	return EmptyToolSchema()
}

func (d CustomToolDecl) ToolCapability() ToolCapability {
	return ""
}

func (d CustomToolDecl) CapabilityConfig() ToolCapabilityConfig {
	return EmptyToolCapabilityConfig()
}

// CapabilityToolDecl represents a provider-independent semantic capability.
type CapabilityToolDecl struct {
	ID         SemanticToolID
	Capability ToolCapability
	Config     ToolCapabilityConfig
	Execution  ToolExecutionOwner
}

// NewCapabilityToolDecl normalizes one provider-independent capability
// declaration into canonical form.
func NewCapabilityToolDecl(id string, capability ToolCapability, config ToolCapabilityConfig) CapabilityToolDecl {
	return CapabilityToolDecl{
		ID:         NewSemanticToolIDFor(ToolOriginRequest, ToolKindCapability, id),
		Capability: NewToolCapability(capability.String()),
		Config:     NewToolCapabilityConfigObject(config.RawObject()),
		Execution:  ToolOwnerClient,
	}
}

func (d CapabilityToolDecl) toolDecl() {}

func (d CapabilityToolDecl) ToolID() SemanticToolID {
	return normalizeSemanticToolID(d.ID, ToolOriginRequest, ToolKindCapability, d.ToolName())
}

func (d CapabilityToolDecl) Owner() ToolExecutionOwner {
	return normalizeToolExecutionOwner(d.Execution)
}

func (d CapabilityToolDecl) Clone() ToolDecl {
	return CapabilityToolDecl{
		ID:         d.ID.Clone(),
		Capability: NewToolCapability(d.Capability.String()),
		Config:     NewToolCapabilityConfigObject(d.Config.RawObject()),
		Execution:  normalizeToolExecutionOwner(d.Execution),
	}
}

func (d CapabilityToolDecl) ToolName() string {
	return d.Capability.String()
}

func (d CapabilityToolDecl) ToolDescription() string {
	return ""
}

func (d CapabilityToolDecl) ToolInputSchema() ToolSchema {
	return EmptyToolSchema()
}

func (d CapabilityToolDecl) ToolCapability() ToolCapability {
	return NewToolCapability(d.Capability.String())
}

func (d CapabilityToolDecl) CapabilityConfig() ToolCapabilityConfig {
	return NewToolCapabilityConfigObject(d.Config.RawObject())
}

type ToolPolicyMode string

// ToolPolicyMode enumerates the supported tool-intent lowerings.
//
// none is the explicit tool-forbidden mode, auto permits zero or more tool
// calls from the declared surface, required demands at least one tool call,
// and specific forces one exact tool by full ToolID.
//
// ChoiceAllowed remains a reserved canonical extension, not a current mode:
// it would represent an exact allowed ToolID subset plus requiredness, and we
// only surface it once a supported wire family can carry that selection
// constraint losslessly without widening, flattening, or renaming the tool
// surface.
const (
	ToolPolicyNone     ToolPolicyMode = "none"
	ToolPolicyAuto     ToolPolicyMode = "auto"
	ToolPolicyRequired ToolPolicyMode = "required"
	ToolPolicySpecific ToolPolicyMode = "specific"
)

// ParseToolPolicyMode parses one raw tool-policy mode without silently
// defaulting unknown values.
func ParseToolPolicyMode(raw string) (ToolPolicyMode, bool) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=domain
	normalized := strings.ToLower(trimmed)
	if normalized == "none" {
		return ToolPolicyNone, true
	}
	if normalized == "auto" {
		return ToolPolicyAuto, true
	}
	if normalized == "required" {
		return ToolPolicyRequired, true
	}
	if normalized == "specific" {
		return ToolPolicySpecific, true
	}
	return "", false
}

func normalizeToolPolicyMode(mode ToolPolicyMode) ToolPolicyMode {
	if parsed, ok := ParseToolPolicyMode(string(mode)); ok {
		return parsed
	}
	return ToolPolicyNone
}

// ToolPolicy describes what the caller wants the model to do with tools.
//
// Mode carries the explicit semantic choice: none forbids tools, auto allows
// optional tool use, required forces at least one tool call, and specific
// forces one exact tool.
type ToolPolicy struct {
	Mode         ToolPolicyMode
	Specific     *SemanticToolID
	SpecificType string
}

// NewToolPolicy normalizes one semantic tool policy into canonical form.
func NewToolPolicy(mode ToolPolicyMode, specific *SemanticToolID) ToolPolicy {
	policy := ToolPolicy{Mode: normalizeToolPolicyMode(mode)}
	if specific != nil {
		id := specific.Clone()
		policy.Mode = ToolPolicySpecific
		policy.Specific = &id
	}
	return policy
}

func (p ToolPolicy) Clone() ToolPolicy {
	cloned := NewToolPolicy(p.Mode, p.Specific)
	cloned.SpecificType = strings.ToLower(strings.TrimSpace(p.SpecificType)) // swobu:io-string source=domain
	return cloned
}

func (p ToolPolicy) IsZero() bool {
	return p.Mode == ToolPolicyNone && p.Specific == nil
}

func (p ToolPolicy) SpecificID() (SemanticToolID, bool) {
	if p.Specific == nil || p.Specific.IsZero() {
		return SemanticToolID{}, false
	}
	return p.Specific.Clone(), true
}

// SpecificToolType reports the requested wire tool type when a specific tool
// selection was decoded from a protocol that preserves it.
func (p ToolPolicy) SpecificToolType() (string, bool) {
	trimmed := strings.ToLower(strings.TrimSpace(p.SpecificType))
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func (p ToolPolicy) Validate() error {
	switch p.Mode {
	case ToolPolicyNone, ToolPolicyAuto, ToolPolicyRequired:
		if p.Specific != nil && !p.Specific.IsZero() {
			return BadRequest("tool policy specific tool requires specific mode")
		}
		return nil
	case ToolPolicySpecific:
		if p.Specific == nil || p.Specific.IsZero() {
			return BadRequest("tool policy specific mode requires a tool id")
		}
		return nil
	default:
		return BadRequest("tool policy mode is invalid")
	}
}

func cloneBoolPointer(ptr *bool) *bool {
	if ptr == nil {
		return nil
	}
	cloned := *ptr
	return &cloned
}
