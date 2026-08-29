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
	byWire      map[string]canonical.ToolKey
}

// BuildAttemptToolNames allocates labels from the complete post-MCP semantic
// context. Ordinary namespace children and historical calls participate; MCP
// catalog children participate only after Exchange materializes them as
// ordinary functions, while fixed built-ins do not.
func BuildAttemptToolNames(semantic canonical.CanonicalRequest) (AttemptToolNames, []compat.Change, error) {
	return buildAttemptToolNames(semantic, toolname.Generated)
}

func buildAttemptToolNames(
	semantic canonical.CanonicalRequest,
	generated func(string, []string, string) string,
) (AttemptToolNames, []compat.Change, error) {
	names := AttemptToolNames{
		byCanonical: make(map[canonical.ToolKey]string),
		byWire:      make(map[string]canonical.ToolKey),
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
		if call, ok := item.ToolCall(); ok && attemptCallable(call) {
			keys = appendUniqueToolKey(keys, call.Tool())
		}
	}
	baseNames := make(map[canonical.ToolKey]string, len(keys))
	baseCounts := make(map[string]int, len(keys))
	for _, key := range keys {
		wireName := key.Name()
		if key.Namespace() != canonical.ToolNamespaceRequest || !toolname.PreservableLiteral(wireName) {
			scope := readableScope(key.Namespace())
			wireName = generated(key.String(), scope, key.Name())
		}
		baseNames[key] = wireName
		baseCounts[wireName]++
	}
	changes := make([]compat.Change, 0)
	usedWireNames := make(map[string]canonical.ToolKey)
	for _, key := range keys {
		wireName := baseNames[key]
		if baseCounts[wireName] > 1 {
			scope := readableScope(key.Namespace())
			wireName = generated(key.String(), scope, key.Name())
		}
		if prior, exists := usedWireNames[wireName]; exists && prior != key {
			return AttemptToolNames{}, nil, canonical.InternalError("attempt tool wire-name collision")
		}
		if prior, exists := names.byWire[wireName]; exists && prior != key {
			return AttemptToolNames{}, nil, canonical.InternalError("attempt tool wire-name collision")
		}
		names.byCanonical[key.Clone()] = wireName
		names.byWire[wireName] = key.Clone()
		usedWireNames[wireName] = key.Clone()
		if _, ok := declared[key]; ok && wireName != key.Name() {
			changes = append(changes, compat.NewApproximation(
				canonical.RequestToolsName,

				canonical.ToolOccurrence(key)))
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

// CanonicalKey resolves one provider-returned callable label to its canonical
// source key. Wire labels are globally unique per attempt, so provenance—not a
// guessed wire kind—selects the canonical lifecycle.
func (n AttemptToolNames) CanonicalKey(wireName string) (canonical.ToolKey, bool) {
	key, ok := n.byWire[wireName]
	return key.Clone(), ok
}

func appendAttemptDeclarationKeys(keys *[]canonical.ToolKey, declared map[canonical.ToolKey]struct{}, declaration canonical.ToolDeclaration) {
	switch declaration.Kind() {
	case canonical.ToolKindFunction, canonical.ToolKindCustom:
		key := declaration.Key()
		*keys = appendUniqueToolKey(*keys, key)
		declared[key] = struct{}{}
	case canonical.ToolKindDiscovery:
		discovery, _ := declaration.Discovery()
		if discovery.Executor() == canonical.DiscoveryExecutorClient {
			key := declaration.Key()
			*keys = appendUniqueToolKey(*keys, key)
			declared[key] = struct{}{}
		}
	case canonical.ToolKindNamespace:
		namespace, _ := declaration.Namespace()
		for _, child := range namespace.Tools() {
			appendAttemptDeclarationKeys(keys, declared, child)
		}
	case canonical.ToolKindMCP, canonical.ToolKindWebSearch:
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

func attemptCallable(call canonical.ToolCallItem) bool {
	key := call.Tool()
	if key.Kind() == canonical.ToolKindFunction || key.Kind() == canonical.ToolKindCustom {
		return true
	}
	executor, ok := call.DiscoveryExecutor()
	return ok && executor == canonical.DiscoveryExecutorClient
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
