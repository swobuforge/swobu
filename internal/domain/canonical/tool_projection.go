package canonical

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base32"
	"strings"
)

const (
	toolProjectionKey          = "swobu-canonical-tool-projection-v1"
	toolProjectionDigestLength = 10
	toolProjectionSeparator    = "__"
	toolProjectionKindFunction = ToolTypeFunction
	toolProjectionKindCustom   = ToolTypeCustom
)

var toolProjectionEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// ProjectedToolName renders one canonical function/custom tool declaration as a
// deterministic wire-safe flat name.
//
// The projection is reversible because the stable HMAC key binds the projected
// suffix to the canonical tool identity without requiring persisted projection
// state.
func ProjectedToolName(tool ToolDecl) (string, error) {
	if tool == nil {
		return "", BadRequest("canonical request tool declarations are invalid")
	}
	return projectedToolNameForID(tool.ToolID(), ToolDeclKind(tool))
}

// projectedToolNameForID renders one canonical function/custom tool identity as
// a deterministic wire-safe flat name.
// swobu:lint ignore string-switch because=projection accepts boundary tool-kind text.
func projectedToolNameForID(id SemanticToolID, kind string) (string, error) {
	normalizedKind := strings.ToLower(strings.TrimSpace(kind)) // swobu:io-string source=domain
	switch normalizedKind {
	case toolProjectionKindFunction, toolProjectionKindCustom:
	default:
		return "", UnsupportedOperation("canonical tool projection only supports function and custom tools")
	}

	normalizedID := NewSemanticToolIDFor(ToolOriginRequest, ToolKind(normalizedKind), id.Path)
	if strings.TrimSpace(normalizedID.Path) == "" { // swobu:io-string source=domain
		return "", BadRequest("canonical tool projection requires a tool path")
	}
	namespace, leaf := splitProjectedToolPath(normalizedID.Path)
	if leaf == "" {
		return "", BadRequest("canonical tool projection requires a tool name")
	}
	if namespace == "" {
		return leaf, nil
	}
	digest := toolProjectionDigest(normalizedID.String())
	return namespace + toolProjectionSeparator + leaf + toolProjectionSeparator + digest, nil
}

// ParseProjectedToolName resolves one projected wire name back to canonical
// structured tool identity.
// swobu:lint ignore string-switch because=projection parses boundary tool-kind text.
func ParseProjectedToolName(raw string, kind ToolKind) (SemanticToolID, string, error) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=domain
	if trimmed == "" {
		return SemanticToolID{}, "", BadRequest("canonical request tool references require a name")
	}
	normalizedKind := strings.ToLower(strings.TrimSpace(string(kind))) // swobu:io-string source=domain
	switch normalizedKind {
	case toolProjectionKindFunction, toolProjectionKindCustom:
	default:
		return SemanticToolID{}, "", UnsupportedOperation("canonical tool projection only supports function and custom tools")
	}

	hashIndex := strings.LastIndex(trimmed, toolProjectionSeparator)
	if hashIndex <= 0 || hashIndex+len(toolProjectionSeparator) >= len(trimmed) {
		return SemanticToolID{}, "", BadRequest("canonical request tool references are invalid")
	}
	projectedDigest := trimmed[hashIndex+len(toolProjectionSeparator):]
	prefix := trimmed[:hashIndex]

	var (
		foundID   SemanticToolID
		foundLeaf string
		found     bool
	)
	searchFrom := 0
	for {
		splitIndex := strings.Index(prefix[searchFrom:], toolProjectionSeparator)
		if splitIndex < 0 {
			break
		}
		splitIndex += searchFrom
		namespace := prefix[:splitIndex]
		leaf := prefix[splitIndex+len(toolProjectionSeparator):]
		if leaf == "" {
			searchFrom = splitIndex + len(toolProjectionSeparator)
			continue
		}
		candidate := NewSemanticToolIDFor(ToolOriginRequest, ToolKind(normalizedKind), buildProjectedToolPath(namespace, leaf))
		if toolProjectionDigest(candidate.String()) != projectedDigest {
			searchFrom = splitIndex + len(toolProjectionSeparator)
			continue
		}
		if found && (foundID != candidate || foundLeaf != leaf) {
			return SemanticToolID{}, "", BadRequest("canonical request tool references are ambiguous")
		}
		foundID = candidate
		foundLeaf = leaf
		found = true
		searchFrom = splitIndex + len(toolProjectionSeparator)
	}
	if !found {
		return SemanticToolID{}, "", BadRequest("canonical request tool references an undeclared tool")
	}
	return foundID, foundLeaf, nil
}

// ToolIdentityFromWire resolves one flat wire token into a canonical tool ID
// and visible leaf name.
//
// Namespace-bearing projections round-trip to their canonical identity.
// Otherwise the token stays raw.
func ToolIdentityFromWire(raw string, kind ToolKind) (SemanticToolID, string, bool) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if trimmed == "" {
		return SemanticToolID{}, "", false
	}
	id, leaf, ok := verifiedProjectedToolIdentityFromWire(trimmed, kind)
	if ok {
		return id, leaf, true
	}
	return NewSemanticToolIDFor(ToolOriginRequest, kind, trimmed), trimmed, false
}

func verifiedProjectedToolIdentityFromWire(name string, kind ToolKind) (SemanticToolID, string, bool) {
	id, leaf, err := ParseProjectedToolName(strings.TrimSpace(name), kind) // swobu:io-string source=boundary
	if err != nil {
		return SemanticToolID{}, "", false
	}
	if !strings.Contains(strings.TrimSpace(id.Path), "/") { // swobu:io-string source=domain
		return SemanticToolID{}, "", false
	}
	return id, leaf, true
}

func toolProjectionDigest(material string) string {
	mac := hmac.New(sha256.New, []byte(toolProjectionKey))
	_, _ = mac.Write([]byte(material))
	digest := strings.ToLower(toolProjectionEncoding.EncodeToString(mac.Sum(nil))) // swobu:io-string source=domain
	if len(digest) > toolProjectionDigestLength {
		digest = digest[:toolProjectionDigestLength]
	}
	return digest
}

func splitProjectedToolPath(path string) (string, string) {
	trimmed := strings.TrimSpace(path) // swobu:io-string source=domain
	if trimmed == "" {
		return "", ""
	}
	if index := strings.LastIndex(trimmed, "/"); index >= 0 {
		namespace := strings.TrimSpace(trimmed[:index]) // swobu:io-string source=domain
		leaf := strings.TrimSpace(trimmed[index+1:])    // swobu:io-string source=domain
		return namespace, leaf
	}
	return "", trimmed
}

func buildProjectedToolPath(namespace, leaf string) string {
	trimmedNamespace := strings.TrimSpace(namespace) // swobu:io-string source=domain
	trimmedLeaf := strings.TrimSpace(leaf)           // swobu:io-string source=domain
	if trimmedNamespace == "" {
		return trimmedLeaf
	}
	return trimmedNamespace + "/" + trimmedLeaf
}
