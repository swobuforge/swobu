// Package canonicaltest provides checked canonical fixtures for non-canonical tests.
package canonicaltest

import "github.com/swobuforge/swobu/internal/domain/canonical"

func MustRequestToolKey(kind canonical.ToolKind, name string) canonical.ToolKey {
	key, err := canonical.NewRequestToolKey(kind, name)
	if err != nil {
		panic(err)
	}
	return key
}

func MustFunctionTool(key canonical.ToolKey, description string, schema canonical.ToolSchema, strict canonical.Specified[bool]) canonical.ToolDeclaration {
	declaration, err := canonical.NewFunctionTool(key, description, schema, strict)
	if err != nil {
		panic(err)
	}
	return declaration
}

func MustCustomTool(key canonical.ToolKey, description string, format canonical.ToolFormat) canonical.ToolDeclaration {
	declaration, err := canonical.NewCustomTool(key, description, format)
	if err != nil {
		panic(err)
	}
	return declaration
}

func MustMessageStart(role canonical.MessageRole) canonical.ItemStartPayload {
	start, err := canonical.NewMessageStart(role)
	if err != nil {
		panic(err)
	}
	return start
}

func MustToolCallStart(callID canonical.ToolCallID, key canonical.ToolKey) canonical.ItemStartPayload {
	start, err := canonical.NewToolCallStart(callID, key)
	if err != nil {
		panic(err)
	}
	return start
}
