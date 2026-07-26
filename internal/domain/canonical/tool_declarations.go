package canonical

import (
	"fmt"
	"strings"
)

// ToolDeclaration is the closed request-level sum of directly model-available
// tools. New branches are added here only with an owning RFC and real codecs.
type ToolDeclaration struct {
	function  *FunctionTool
	custom    *CustomTool
	namespace *ToolNamespace
	discovery *ToolDiscoveryTool
	// builtIn discriminates only payload-free built-ins. Function and custom
	// identity is derived from the branch that already owns their payload.
	builtIn ToolKind
}

// DiscoveryExecutor identifies who executes a discovery call.
type DiscoveryExecutor uint8

const (
	DiscoveryExecutorClient DiscoveryExecutor = iota + 1
	DiscoveryExecutorProvider
)

// ToolDiscoveryTool declares provider-neutral dynamic tool discovery.
type ToolDiscoveryTool struct {
	description string
	inputSchema ToolSchema
	executor    DiscoveryExecutor
}

// ToolNamespace preserves one ordered namespace tree.
type ToolNamespace struct {
	key         ToolKey
	description string
	tools       []ToolDeclaration
	mcp         *MCPSource
}

// FunctionTool is an ordinary JSON-schema callable declaration.
type FunctionTool struct {
	key         ToolKey
	description string
	inputSchema ToolSchema
	strict      Specified[bool]
}

// CustomTool is a text-input callable declaration with a typed format object.
type CustomTool struct {
	key         ToolKey
	description string
	format      ToolFormat
}

func NewFunctionTool(key ToolKey, description string, inputSchema ToolSchema, strict Specified[bool]) (ToolDeclaration, error) {
	if key.IsZero() || key.Kind() != ToolKindFunction {
		return ToolDeclaration{}, fmt.Errorf("canonical function tool requires a function key")
	}
	if inputSchema.IsEmpty() {
		return ToolDeclaration{}, fmt.Errorf("canonical function tool requires an input schema")
	}
	tool := FunctionTool{key: key.Clone(), description: strings.TrimSpace(description), inputSchema: inputSchema.Clone(), strict: cloneSpecified(strict, func(v bool) bool { return v })} // swobu:io-string source=domain
	return ToolDeclaration{function: &tool}, nil
}

func NewCustomTool(key ToolKey, description string, format ToolFormat) (ToolDeclaration, error) {
	if key.IsZero() || key.Kind() != ToolKindCustom {
		return ToolDeclaration{}, fmt.Errorf("canonical custom tool requires a custom key")
	}
	tool := CustomTool{key: key.Clone(), description: strings.TrimSpace(description), format: format.Clone()} // swobu:io-string source=domain
	return ToolDeclaration{custom: &tool}, nil
}

// NewWebSearchDeclaration constructs the single fixed-key web-search declaration.
func NewWebSearchDeclaration() ToolDeclaration {
	return ToolDeclaration{builtIn: ToolKindWebSearch}
}

func NewToolDiscoveryTool(description string, inputSchema ToolSchema, executor DiscoveryExecutor) (ToolDeclaration, error) {
	if inputSchema.IsEmpty() || (executor != DiscoveryExecutorClient && executor != DiscoveryExecutorProvider) {
		return ToolDeclaration{}, fmt.Errorf("canonical tool discovery declaration is invalid")
	}
	return ToolDeclaration{discovery: &ToolDiscoveryTool{description: strings.TrimSpace(description), inputSchema: inputSchema.Clone(), executor: executor}}, nil
}

func NewToolNamespace(key ToolKey, description string, tools []ToolDeclaration) (ToolDeclaration, error) {
	return newToolNamespace(key, description, tools, nil)
}

func NewMCPToolNamespace(key ToolKey, description string, source MCPSource, tools []ToolDeclaration) (ToolDeclaration, error) {
	return newToolNamespace(key, description, tools, &source)
}

func newToolNamespace(key ToolKey, description string, tools []ToolDeclaration, source *MCPSource) (ToolDeclaration, error) {
	if key.IsZero() || key.Kind() != ToolKindNamespace {
		return ToolDeclaration{}, fmt.Errorf("canonical tool namespace declaration is invalid")
	}
	for _, child := range tools {
		if namespace, ok := child.Namespace(); ok {
			if _, isMCP := namespace.MCPSource(); isMCP {
				return ToolDeclaration{}, fmt.Errorf("canonical remote tool namespace cannot be nested")
			}
		}
	}
	children, err := NewToolSet(tools)
	if err != nil {
		return ToolDeclaration{}, fmt.Errorf("canonical tool namespace declaration is invalid: %w", err)
	}
	namespace := ToolNamespace{key: key.Clone(), description: strings.TrimSpace(description), tools: children.Declarations()}
	if source != nil {
		cloned := source.Clone()
		namespace.mcp = &cloned
	}
	return ToolDeclaration{namespace: &namespace}, nil
}

func (d ToolDeclaration) Kind() ToolKind {
	switch {
	case d.function != nil && d.custom == nil && d.namespace == nil && d.discovery == nil && d.builtIn == "":
		return ToolKindFunction
	case d.function == nil && d.custom != nil && d.namespace == nil && d.discovery == nil && d.builtIn == "":
		return ToolKindCustom
	case d.function == nil && d.custom == nil && d.namespace != nil && d.discovery == nil && d.builtIn == "":
		return ToolKindNamespace
	case d.function == nil && d.custom == nil && d.namespace == nil && d.discovery != nil && d.builtIn == "":
		return ToolKindDiscovery
	case d.function == nil && d.custom == nil && d.namespace == nil && d.discovery == nil && d.builtIn == ToolKindWebSearch:
		return ToolKindWebSearch
	default:
		return ""
	}
}

func (d ToolDeclaration) Key() ToolKey {
	if d.Kind() == ToolKindFunction {
		return d.function.key.Clone()
	}
	if d.Kind() == ToolKindCustom {
		return d.custom.key.Clone()
	}
	if d.Kind() == ToolKindWebSearch {
		return WebSearchToolKey()
	}
	if d.Kind() == ToolKindNamespace {
		return d.namespace.key.Clone()
	}
	if d.Kind() == ToolKindDiscovery {
		return ToolDiscoveryKey()
	}
	return ToolKey{}
}

func (d ToolDeclaration) Function() (FunctionTool, bool) {
	if d.Kind() != ToolKindFunction {
		return FunctionTool{}, false
	}
	return d.function.Clone(), true
}

func (d ToolDeclaration) Custom() (CustomTool, bool) {
	if d.Kind() != ToolKindCustom {
		return CustomTool{}, false
	}
	return d.custom.Clone(), true
}

func (d ToolDeclaration) Namespace() (ToolNamespace, bool) {
	if d.Kind() != ToolKindNamespace {
		return ToolNamespace{}, false
	}
	return d.namespace.Clone(), true
}

func (d ToolDeclaration) Discovery() (ToolDiscoveryTool, bool) {
	if d.Kind() != ToolKindDiscovery {
		return ToolDiscoveryTool{}, false
	}
	return d.discovery.Clone(), true
}

func (d ToolDeclaration) Clone() ToolDeclaration {
	if f, ok := d.Function(); ok {
		return ToolDeclaration{function: &f}
	}
	if c, ok := d.Custom(); ok {
		return ToolDeclaration{custom: &c}
	}
	if d.Kind() == ToolKindWebSearch {
		return NewWebSearchDeclaration()
	}
	if namespace, ok := d.Namespace(); ok {
		return ToolDeclaration{namespace: &namespace}
	}
	if discovery, ok := d.Discovery(); ok {
		return ToolDeclaration{discovery: &discovery}
	}
	return ToolDeclaration{}
}

// Equivalent reports semantic declaration equality for environment folding.
func (d ToolDeclaration) Equivalent(other ToolDeclaration) bool {
	if d.Kind() != other.Kind() || d.Key() != other.Key() {
		return false
	}
	if left, ok := d.Function(); ok {
		right, rightOK := other.Function()
		if !rightOK || left.Description() != right.Description() ||
			left.InputSchema().RawObject() != right.InputSchema().RawObject() {
			return false
		}
		leftStrict, leftSet := left.Strict().Get()
		rightStrict, rightSet := right.Strict().Get()
		if leftSet != rightSet || leftSet && leftStrict != rightStrict {
			return false
		}
		return true
	}
	if left, ok := d.Custom(); ok {
		right, rightOK := other.Custom()
		return rightOK && left.Description() == right.Description() &&
			left.Format().RawObject() == right.Format().RawObject()
	}
	if left, ok := d.Namespace(); ok {
		right, rightOK := other.Namespace()
		if !rightOK || left.Description() != right.Description() || len(left.Tools()) != len(right.Tools()) {
			return false
		}
		leftMCP, leftMCPSet := left.MCPSource()
		rightMCP, rightMCPSet := right.MCPSource()
		if leftMCPSet != rightMCPSet || leftMCPSet && !leftMCP.Equivalent(rightMCP) {
			return false
		}
		leftTools := left.Tools()
		rightTools := right.Tools()
		for index := range leftTools {
			if !leftTools[index].Equivalent(rightTools[index]) {
				return false
			}
		}
		return true
	}
	if left, ok := d.Discovery(); ok {
		right, rightOK := other.Discovery()
		return rightOK && left.Description() == right.Description() &&
			left.InputSchema().RawObject() == right.InputSchema().RawObject() &&
			left.Executor() == right.Executor()
	}
	return d.Kind() == ToolKindWebSearch
}

func (n ToolNamespace) Key() ToolKey             { return n.key.Clone() }
func (n ToolNamespace) Description() string      { return n.description }
func (n ToolNamespace) Tools() []ToolDeclaration { return cloneToolDeclarations(n.tools) }
func (n ToolNamespace) Clone() ToolNamespace {
	cloned := ToolNamespace{key: n.key.Clone(), description: n.description, tools: cloneToolDeclarations(n.tools)}
	if n.mcp != nil {
		source := n.mcp.Clone()
		cloned.mcp = &source
	}
	return cloned
}
func (n ToolNamespace) MCPSource() (MCPSource, bool) {
	if n.mcp == nil {
		return MCPSource{}, false
	}
	return n.mcp.Clone(), true
}
func (d ToolDiscoveryTool) Description() string         { return d.description }
func (d ToolDiscoveryTool) InputSchema() ToolSchema     { return d.inputSchema.Clone() }
func (d ToolDiscoveryTool) Executor() DiscoveryExecutor { return d.executor }
func (d ToolDiscoveryTool) Clone() ToolDiscoveryTool {
	return ToolDiscoveryTool{description: d.description, inputSchema: d.inputSchema.Clone(), executor: d.executor}
}

func (f FunctionTool) Key() ToolKey            { return f.key.Clone() }
func (f FunctionTool) Description() string     { return f.description }
func (f FunctionTool) InputSchema() ToolSchema { return f.inputSchema.Clone() }
func (f FunctionTool) Strict() Specified[bool] {
	return cloneSpecified(f.strict, func(v bool) bool { return v })
}
func (f FunctionTool) Clone() FunctionTool {
	return FunctionTool{key: f.key.Clone(), description: f.description, inputSchema: f.inputSchema.Clone(), strict: f.Strict()}
}

func (c CustomTool) Key() ToolKey        { return c.key.Clone() }
func (c CustomTool) Description() string { return c.description }
func (c CustomTool) Format() ToolFormat  { return c.format.Clone() }
func (c CustomTool) Clone() CustomTool {
	return CustomTool{key: c.key.Clone(), description: c.description, format: c.format.Clone()}
}
