package canonical

import "strings"

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
