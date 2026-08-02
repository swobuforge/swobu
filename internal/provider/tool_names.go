package provider

import (
	"fmt"
	"strings"

	"github.com/swobuforge/swobu/internal/compat"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/wire/toolname"
)

// AttemptToolNames is one provider attempt's immutable bidirectional mapping
// between canonical callable identity and provider-valid wire labels. It never
// enters canonical history, checkpoint state, sessions, or durable config.
type AttemptToolNames struct {
	byCanonical map[canonical.ToolKey]string
	byWire      map[wireToolRef]canonical.ToolKey
}

type wireToolRef struct {
	kind canonical.ToolKind
	name string
}

// BuildAttemptToolNames allocates labels from the complete post-MCP semantic
// context. Ordinary namespace children and historical calls participate;
// residual native MCP catalogs and fixed built-ins do not.
func BuildAttemptToolNames(semantic canonical.CanonicalRequest) (AttemptToolNames, []compat.Change, error) {
	return buildAttemptToolNames(semantic, toolname.Generated)
}

func buildAttemptToolNames(
	semantic canonical.CanonicalRequest,
	generated func(string, []string, string) string,
) (AttemptToolNames, []compat.Change, error) {
	names := AttemptToolNames{
		byCanonical: make(map[canonical.ToolKey]string),
		byWire:      make(map[wireToolRef]canonical.ToolKey),
	}
	keys := make([]canonical.ToolKey, 0)
	declared := make(map[canonical.ToolKey]struct{})
	environment, err := canonical.EffectiveTools(semantic)
	if err != nil {
		return AttemptToolNames{}, nil, err
	}
	for _, declaration := range environment.Declarations() {
		appendAttemptDeclarationKeys(&keys, declared, declaration)
	}
	for _, item := range semantic.Items() {
		if call, ok := item.ToolCall(); ok && attemptCallable(call.Tool()) {
			keys = appendUniqueToolKey(keys, call.Tool())
		}
	}
	changes := make([]compat.Change, 0)
	for _, key := range keys {
		wireName := key.Name()
		if key.Namespace() != canonical.ToolNamespaceRequest || !toolname.PreservableLiteral(wireName) {
			scope := readableScope(key.Namespace())
			wireName = generated(key.String(), scope, key.Name())
		}
		ref := wireToolRef{kind: key.Kind(), name: wireName}
		if prior, exists := names.byWire[ref]; exists && prior != key {
			return AttemptToolNames{}, nil, canonical.InternalError("attempt tool wire-name collision")
		}
		names.byCanonical[key.Clone()] = wireName
		names.byWire[ref] = key.Clone()
		if _, ok := declared[key]; ok && wireName != key.Name() {
			changes = append(changes, compat.NewApproximation(
				canonical.RequestToolsName,
				canonical.RequestTools,
				canonical.ToolOccurrence(key),
			))
		}
	}
	return names, changes, nil
}

// WireName returns the exact provider label allocated for key.
func (n AttemptToolNames) WireName(key canonical.ToolKey) (string, error) {
	name, ok := n.byCanonical[key]
	if !ok {
		return "", fmt.Errorf("attempt tool names have no canonical key %q", key.String())
	}
	return name, nil
}

// CanonicalKey resolves one provider-returned callable label for this attempt.
func (n AttemptToolNames) CanonicalKey(kind canonical.ToolKind, wireName string) (canonical.ToolKey, bool) {
	key, ok := n.byWire[wireToolRef{kind: kind, name: wireName}]
	return key.Clone(), ok
}

func appendAttemptDeclarationKeys(keys *[]canonical.ToolKey, declared map[canonical.ToolKey]struct{}, declaration canonical.ToolDeclaration) {
	switch declaration.Kind() {
	case canonical.ToolKindFunction, canonical.ToolKindCustom:
		key := declaration.Key()
		*keys = appendUniqueToolKey(*keys, key)
		declared[key] = struct{}{}
	case canonical.ToolKindNamespace:
		namespace, _ := declaration.Namespace()
		for _, child := range namespace.Tools() {
			appendAttemptDeclarationKeys(keys, declared, child)
		}
	case canonical.ToolKindMCP, canonical.ToolKindWebSearch, canonical.ToolKindDiscovery:
		return
	}
}

func appendUniqueToolKey(keys []canonical.ToolKey, key canonical.ToolKey) []canonical.ToolKey {
	for _, existing := range keys {
		if existing == key {
			return keys
		}
	}
	return append(keys, key.Clone())
}

func attemptCallable(key canonical.ToolKey) bool {
	return key.Kind() == canonical.ToolKindFunction || key.Kind() == canonical.ToolKindCustom
}

func readableScope(namespace string) []string {
	parts := strings.Split(strings.Trim(namespace, "/"), "/")
	for len(parts) > 0 && parts[0] == "request" {
		parts = parts[1:]
	}
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return parts
}
