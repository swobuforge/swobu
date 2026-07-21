package canonical

import "strings"

const (
	ToolTypeFunction     = "function"
	ToolTypeCustom       = "custom"
	ToolTypeWebSearch    = "web_search"
	ToolNamespaceRequest = "request"
)

// ToolKind identifies the semantic kind of one tool.
type ToolKind string

const (
	ToolKindFunction  ToolKind = "function"
	ToolKindCustom    ToolKind = "custom"
	ToolKindWebSearch ToolKind = "web_search"
)

// ToolKey is the single canonical identity shared by a declaration and calls
// selecting that declaration. Its stable string is derived from namespace,
// kind, and name; those facts cannot contradict an independently supplied ID.
type ToolKey struct {
	namespace string
	kind      ToolKind
	name      string
}

// NewToolKey validates one semantic tool identity.
func NewToolKey(namespace string, kind ToolKind, name string) (ToolKey, error) {
	namespace = strings.Trim(strings.TrimSpace(namespace), "/") // swobu:io-string source=domain
	name = strings.Trim(strings.TrimSpace(name), "/")           // swobu:io-string source=domain
	if namespace == "" || name == "" {
		return ToolKey{}, BadRequest("canonical tool key requires namespace and name")
	}
	if kind != ToolKindFunction && kind != ToolKindCustom && kind != ToolKindWebSearch {
		return ToolKey{}, BadRequest("canonical tool key kind is invalid")
	}
	if strings.Contains(namespace, "//") || strings.Contains(name, "/") {
		return ToolKey{}, BadRequest("canonical tool key namespace or name is invalid")
	}
	return ToolKey{namespace: namespace, kind: kind, name: name}, nil
}

// WebSearchToolKey returns the one fixed request identity for the built-in
// exchange-resolved web-search tool. Wire aliases and provider versions must
// normalize to this key rather than creating new canonical identities.
func WebSearchToolKey() ToolKey {
	key, err := NewRequestToolKey(ToolKindWebSearch, ToolTypeWebSearch)
	if err != nil {
		panic(err)
	}
	return key
}

// NewRequestToolKey constructs a request-scoped key from a possibly
// namespace-qualified source name. The last path segment is the tool name.
func NewRequestToolKey(kind ToolKind, qualifiedName string) (ToolKey, error) {
	if qualifiedName == "" || strings.TrimSpace(qualifiedName) != qualifiedName || strings.HasPrefix(qualifiedName, "/") || strings.HasSuffix(qualifiedName, "/") { // swobu:io-string source=domain
		return ToolKey{}, BadRequest("canonical request tool key is invalid")
	}
	if parsed, err := ParseToolKey(qualifiedName); err == nil && parsed.Kind() == kind {
		return parsed, nil
	}
	namespace := ToolNamespaceRequest
	name := qualifiedName
	if index := strings.LastIndex(qualifiedName, "/"); index >= 0 {
		namespace = qualifiedName[:index]
		name = qualifiedName[index+1:]
	}
	return NewToolKey(namespace, kind, name)
}

// ParseToolKey parses the stable external form tool:v1/{namespace}/{kind}/{name}.
func ParseToolKey(raw string) (ToolKey, error) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=domain
	const prefix = "tool:v1/"
	if !strings.HasPrefix(trimmed, prefix) {
		return ToolKey{}, BadRequest("canonical tool key is invalid")
	}
	rest := strings.TrimPrefix(trimmed, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) < 3 {
		return ToolKey{}, BadRequest("canonical tool key is invalid")
	}
	namespace := strings.Join(parts[:len(parts)-2], "/")
	return NewToolKey(namespace, ToolKind(parts[len(parts)-2]), parts[len(parts)-1])
}

func (id ToolKey) IsZero() bool {
	return id.namespace == "" && id.kind == "" && id.name == ""
}

func (id ToolKey) String() string {
	if id.namespace != "" && id.kind != "" && id.name != "" {
		return "tool:v1/" + id.namespace + "/" + string(id.kind) + "/" + id.name
	}
	return ""
}

func (id ToolKey) Clone() ToolKey {
	return id
}

func (id ToolKey) Namespace() string { return id.namespace }
func (id ToolKey) Kind() ToolKind    { return id.kind }
func (id ToolKey) Name() string      { return id.name }

// ToolFormat stores semantic custom-tool formatting as one JSON object
// payload.
type ToolFormat struct {
	object *JSONObject
}

// EmptyToolFormat returns an empty custom-tool format.
func EmptyToolFormat() ToolFormat {
	return ToolFormat{}
}

// NewToolFormatObject normalizes one custom-tool format JSON object into
// canonical form.
func NewToolFormatObject(object JSONObject) ToolFormat {
	cloned := object.Clone()
	return ToolFormat{object: &cloned}
}

func (f ToolFormat) RawObject() string {
	if f.object == nil {
		return ""
	}
	return f.object.String()
}

func (f ToolFormat) IsEmpty() bool {
	return f.object == nil
}

func (f ToolFormat) Clone() ToolFormat {
	if f.object == nil {
		return EmptyToolFormat()
	}
	return NewToolFormatObject(*f.object)
}

// ResolveToolDeclByID finds one uniquely identified tool declaration in a flat
// tool surface.
func ResolveToolDeclarationByKey(tools []ToolDeclaration, key ToolKey, specificType string) (ToolDeclaration, string, error) {
	if key.IsZero() {
		return ToolDeclaration{}, "", BadRequest("canonical request tool references require a tool key")
	}
	normalizedSpecific := strings.ToLower(strings.TrimSpace(specificType)) // swobu:io-string source=domain
	var (
		found     ToolDeclaration
		foundType string
		matched   bool
	)
	for _, tool := range tools {
		if tool.Key() != key {
			continue
		}
		toolType := string(tool.Kind())
		if normalizedSpecific != "" && toolType != normalizedSpecific {
			continue
		}
		if matched && (found.Key() != tool.Key() || foundType != toolType) {
			return ToolDeclaration{}, "", BadRequest("canonical request tool references are ambiguous")
		}
		found = tool
		foundType = toolType
		matched = true
	}
	if !matched {
		return ToolDeclaration{}, "", BadRequest("canonical request tool references an undeclared tool")
	}
	return found, foundType, nil
}

// ResolveToolDeclarationByName resolves one literal request-scoped wire name.
func ResolveToolDeclarationByName(tools []ToolDeclaration, name string, specificType string) (ToolDeclaration, string, error) {
	trimmed := strings.TrimSpace(name) // swobu:io-string source=domain
	if trimmed == "" {
		return ToolDeclaration{}, "", BadRequest("canonical request tool references require a name")
	}
	normalizedSpecific := strings.ToLower(strings.TrimSpace(specificType)) // swobu:io-string source=domain
	return resolvePlainToolDeclarationByName(tools, trimmed, normalizedSpecific)
}

// ResolveHistoricalToolKeyByName recovers the by-value identity of a client
// transcript call. Current declarations refine the identity when present; an
// absent declaration does not make historical environment data invalid.
// Provider response decoders must continue using ResolveToolDeclarationByName
// against the exact attempted ToolSet.
func ResolveHistoricalToolKeyByName(tools []ToolDeclaration, name string, kind ToolKind) (ToolKey, error) {
	if declaration, _, err := ResolveToolDeclarationByName(tools, name, string(kind)); err == nil {
		return declaration.Key(), nil
	}
	key, err := ToolIdentityFromWire(name, kind)
	if err != nil {
		return ToolKey{}, BadRequest("canonical historical tool call identity is invalid")
	}
	return key, nil
}

// ToolIdentityFromWire preserves a client transcript's literal flat identity.
// Provider aliases are resolved only through the exact attempt-local table.
func ToolIdentityFromWire(raw string, kind ToolKind) (ToolKey, error) {
	if raw == "" || strings.TrimSpace(raw) != raw { // swobu:io-string source=domain
		return ToolKey{}, BadRequest("canonical wire tool identity is invalid")
	}
	return NewRequestToolKey(kind, raw)
}

func resolvePlainToolDeclarationByName(tools []ToolDeclaration, name string, specificType string) (ToolDeclaration, string, error) {
	normalizedSpecific := strings.ToLower(strings.TrimSpace(specificType)) // swobu:io-string source=domain
	var (
		found     ToolDeclaration
		foundType string
		matched   bool
	)
	for _, tool := range tools {
		toolType := string(tool.Kind())
		if normalizedSpecific != "" && toolType != normalizedSpecific {
			continue
		}
		if strings.TrimSpace(tool.Key().Name()) != name { // swobu:io-string source=domain
			continue
		}
		if matched && (found.Key() != tool.Key() || foundType != toolType) {
			return ToolDeclaration{}, "", BadRequest("canonical request tool references are ambiguous")
		}
		found = tool
		foundType = toolType
		matched = true
	}
	if !matched {
		return ToolDeclaration{}, "", BadRequest("canonical request tool references are undeclared tool")
	}
	return found, foundType, nil
}
