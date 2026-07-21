package canonical

import (
	"fmt"
	"strings"
)

// ToolDeclaration is the closed request-level sum of directly model-available
// tools. New branches are added here only with an owning RFC and real codecs.
type ToolDeclaration struct {
	function *FunctionTool
	custom   *CustomTool
	// builtIn discriminates only payload-free built-ins. Function and custom
	// identity is derived from the branch that already owns their payload.
	builtIn ToolKind
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

func (d ToolDeclaration) Kind() ToolKind {
	switch {
	case d.function != nil && d.custom == nil && d.builtIn == "":
		return ToolKindFunction
	case d.function == nil && d.custom != nil && d.builtIn == "":
		return ToolKindCustom
	case d.function == nil && d.custom == nil && d.builtIn == ToolKindWebSearch:
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
	return ToolDeclaration{}
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
