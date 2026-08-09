package messages

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire"
)

// messagesToolReferenceKinds is the native Messages deferred-tool surface.
// Keep declaration encoding, discovery references, and replay visibility on
// this one semantic list so a newly deferrable server tool cannot drift.
var messagesToolReferenceKinds = [...]canonical.ToolKind{
	canonical.ToolKindFunction,
	canonical.ToolKindWebSearch,
}

func messagesToolCanBeDeferred(kind canonical.ToolKind) bool {
	for _, candidate := range messagesToolReferenceKinds {
		if kind == candidate {
			return true
		}
	}
	return false
}

func resolveMessagesProviderToolReference(names wire.ToolNames, environment canonical.ToolEnvironment, name string) (canonical.ToolDeclaration, error) {
	trimmed := strings.TrimSpace(name)
	for _, kind := range messagesToolReferenceKinds {
		if kind == canonical.ToolKindWebSearch && trimmed == canonical.WebSearchToolKey().Name() {
			if declaration, ok := environment.Lookup(canonical.WebSearchToolKey()); ok {
				return declaration, nil
			}
			continue
		}
		key, err := wire.DecodeToolKey(names, environment, kind, trimmed)
		if err == nil {
			declaration, _ := environment.Lookup(key)
			return declaration, nil
		}
	}
	return canonical.ToolDeclaration{}, canonical.NewBackendError("messages", 0, "messages discovery result references an unknown tool", "")
}

func resolveMessagesHistoricalToolReference(tools []canonical.ToolDeclaration, name string) (canonical.ToolDeclaration, error) {
	for _, kind := range messagesToolReferenceKinds {
		declaration, _, err := canonical.ResolveToolDeclarationByName(tools, name, string(kind))
		if err == nil {
			return declaration, nil
		}
	}
	return canonical.ToolDeclaration{}, canonical.BadRequest("messages discovery result references an unknown tool")
}
