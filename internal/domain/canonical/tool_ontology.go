package canonical

import "strings"

// SemanticToolID is the canonical identifier for one semantic tool
// declaration or tool-call target.
type SemanticToolID string

// NewSemanticToolID normalizes one semantic tool identifier into canonical
// form.
func NewSemanticToolID(raw string) SemanticToolID {
	return SemanticToolID(strings.TrimSpace(raw)) // swobu:io-string source=domain
}

func (id SemanticToolID) IsZero() bool {
	return strings.TrimSpace(string(id)) == "" // swobu:io-string source=domain
}

func (id SemanticToolID) String() string {
	return string(id)
}

func (id SemanticToolID) Clone() SemanticToolID {
	return SemanticToolID(string(id))
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
	return ToolCapabilityConfig{rawObject: strings.TrimSpace(raw)} // swobu:io-string source=domain
}

func (c ToolCapabilityConfig) RawObject() string {
	return c.rawObject
}

func (c ToolCapabilityConfig) IsEmpty() bool {
	return strings.TrimSpace(c.rawObject) == "" // swobu:io-string source=domain
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

// FunctionToolDecl represents an ordinary JSON-schema callable tool.
type FunctionToolDecl struct {
	ID          SemanticToolID
	Name        string
	Description string
	InputSchema ToolSchema
	Execution   ToolExecutionOwner
}

// NewFunctionToolDecl normalizes one callable tool declaration into canonical
// form.
func NewFunctionToolDecl(id, name, description string, inputSchema ToolSchema) FunctionToolDecl {
	normalized := FunctionToolDecl{
		ID:          NewSemanticToolID(id),
		Name:        strings.TrimSpace(name),        // swobu:io-string source=domain
		Description: strings.TrimSpace(description), // swobu:io-string source=domain
		InputSchema: NewToolSchemaObject(inputSchema.RawObject()),
		Execution:   ToolOwnerClient,
	}
	if normalized.ID.IsZero() {
		normalized.ID = NewSemanticToolID(normalized.Name)
	}
	return normalized
}

func (d FunctionToolDecl) toolDecl() {}

func (d FunctionToolDecl) ToolID() SemanticToolID {
	return d.ID.Clone()
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
		ID:         NewSemanticToolID(id),
		Capability: NewToolCapability(capability.String()),
		Config:     NewToolCapabilityConfigObject(config.RawObject()),
		Execution:  ToolOwnerClient,
	}
}

func (d CapabilityToolDecl) toolDecl() {}

func (d CapabilityToolDecl) ToolID() SemanticToolID {
	return d.ID.Clone()
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
const (
	ToolPolicyNone     ToolPolicyMode = "none"
	ToolPolicyAuto     ToolPolicyMode = "auto"
	ToolPolicyRequired ToolPolicyMode = "required"
	ToolPolicySpecific ToolPolicyMode = "specific"
)

func normalizeToolPolicyMode(mode ToolPolicyMode) ToolPolicyMode {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case "", "none":
		return ToolPolicyNone
	case "auto":
		return ToolPolicyAuto
	case "required":
		return ToolPolicyRequired
	case "specific":
		return ToolPolicySpecific
	default:
		return ToolPolicyNone
	}
}

// ToolPolicy describes what the caller wants the model to do with tools.
type ToolPolicy struct {
	Mode     ToolPolicyMode
	Specific *SemanticToolID
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
	return NewToolPolicy(p.Mode, p.Specific)
}

func (p ToolPolicy) IsZero() bool {
	return p.Mode == ToolPolicyNone && p.Specific == nil
}

func (p ToolPolicy) SpecificID() (SemanticToolID, bool) {
	if p.Specific == nil || p.Specific.IsZero() {
		return "", false
	}
	return p.Specific.Clone(), true
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
