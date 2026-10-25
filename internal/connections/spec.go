// Package connections owns reusable upstream identity and revisioned lifecycle.
// Specs are secret-free configuration; checks are observations and do not mutate
// them. Workspace references and credential material remain independently owned.
package connections

import (
	"errors"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
)

// Kind is the closed protocol purpose of a reusable upstream identity.
type Kind string

const (
	Model  Kind = "model"
	MCP    Kind = "mcp"
	Search Kind = "search"
)

// Spec is sealed to validated immutable values. Endpoint syntax does not prove
// network safety: a check adapter must admit resolved addresses on every dial.
type Spec interface {
	Kind() Kind
	isSpec()
}

// ModelSpec preserves the established provider-specific durable sum. Model IDs
// and target-local capability options remain on targets, not Connections.
type ModelSpec struct{ upstream routing.Connection }

func NewModelSpec(upstream routing.Connection) (ModelSpec, error) {
	switch value := upstream.(type) {
	case routing.StandardConnection:
		if value.Provider() == "" {
			return ModelSpec{}, errors.New("model provider is required")
		}
	case routing.ZAIConnection:
		if value.Provider() == "" {
			return ModelSpec{}, errors.New("model provider is required")
		}
	case routing.BedrockConnection:
		if value.Provider() == "" {
			return ModelSpec{}, errors.New("model provider is required")
		}
	case routing.CustomConnection:
		if value.Provider() == "" {
			return ModelSpec{}, errors.New("model provider is required")
		}
	default:
		return ModelSpec{}, errors.New("validated model upstream is required")
	}
	return ModelSpec{upstream: upstream}, nil
}
func (ModelSpec) Kind() Kind                     { return Model }
func (ModelSpec) isSpec()                        {}
func (s ModelSpec) Upstream() routing.Connection { return s.upstream }

// Endpoint excludes credentials, queries and fragments so configuration and
// diagnostics cannot carry URL tokens. Scheme admission is deliberately exact.
type Endpoint struct{ value string }

func ParseEndpoint(raw string) (Endpoint, error) {
	parsed, err := url.Parse(raw)
	if err != nil || raw == "" || raw != strings.TrimSpace(raw) || parsed.User != nil || parsed.Opaque != "" || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || strings.ContainsAny(raw, "\r\n\\") {
		return Endpoint{}, errors.New("endpoint must be an HTTP(S) URL without credentials, query or fragment")
	}
	return Endpoint{value: parsed.String()}, nil
}
func (e Endpoint) String() string { return e.value }

// Credential identifies a supported locator, never a secret value. The owning
// adapter still validates its scope and resolves it only during execution.
func validateCredential(ref credentialref.Ref) error {
	if ref.Kind() == credentialref.KindEmpty {
		return nil
	}
	if ref.Kind() == credentialref.KindOther || ref.IsEmptyFileSelection() || strings.ContainsAny(ref.String(), "\r\n") {
		return errors.New("credential reference is invalid")
	}
	_, payload, hasScheme := strings.Cut(ref.String(), ":")
	if hasScheme && strings.TrimSpace(payload) == "" {
		return errors.New("credential reference payload is required")
	}
	return nil
}

type MCPHTTPSpec struct {
	declarations mcpDeclarations
	endpoint     Endpoint
	credential   credentialref.Ref
}

func NewMCPHTTPSpec(endpoint Endpoint, credential credentialref.Ref) (MCPHTTPSpec, error) {
	if endpoint.value == "" {
		return MCPHTTPSpec{}, errors.New("MCP endpoint is required")
	}
	if err := validateCredential(credential); err != nil {
		return MCPHTTPSpec{}, err
	}
	return MCPHTTPSpec{endpoint: endpoint, credential: credential}, nil
}
func (MCPHTTPSpec) Kind() Kind                      { return MCP }
func (MCPHTTPSpec) isSpec()                         {}
func (s MCPHTTPSpec) Endpoint() Endpoint            { return s.endpoint }
func (s MCPHTTPSpec) Credential() credentialref.Ref { return s.credential }

// MCPStdioSpec stores an executable and argv, never an interpolated shell
// command. Environment values are references. Public projections must redact
// argv and environment locators; this spec is local-only execution intent.
type MCPStdioSpec struct {
	declarations mcpDeclarations
	executable   string
	args         []string
	environment  map[string]credentialref.Ref
}

func NewMCPStdioSpec(executable string, args []string, environment map[string]credentialref.Ref) (MCPStdioSpec, error) {
	if executable == "" || strings.TrimSpace(executable) != executable || strings.ContainsAny(executable, "\x00\r\n") {
		return MCPStdioSpec{}, errors.New("stdio executable is required")
	}
	// Bare executable names and exact paths are allowed; shell command strings
	// containing arguments must be split by the caller into executable and argv.
	if !filepath.IsAbs(executable) && strings.ContainsAny(executable, " \t") {
		return MCPStdioSpec{}, errors.New("stdio executable must not contain shell arguments")
	}
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return MCPStdioSpec{}, errors.New("stdio argument contains NUL")
		}
	}
	env := make(map[string]credentialref.Ref, len(environment))
	for name, ref := range environment {
		if !environmentName(name) || ref.Kind() == credentialref.KindEmpty {
			return MCPStdioSpec{}, errors.New("stdio environment requires names and credential references")
		}
		if err := validateCredential(ref); err != nil {
			return MCPStdioSpec{}, err
		}
		env[name] = ref
	}
	return MCPStdioSpec{executable: executable, args: append([]string(nil), args...), environment: env}, nil
}
func environmentName(name string) bool {
	if name == "" {
		return false
	}
	for i, c := range name {
		if c != '_' && !(c >= 'A' && c <= 'Z') && !(c >= 'a' && c <= 'z') && !(i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func (MCPStdioSpec) Kind() Kind           { return MCP }
func (MCPStdioSpec) isSpec()              {}
func (s MCPStdioSpec) Executable() string { return s.executable }
func (s MCPStdioSpec) Args() []string     { return append([]string(nil), s.args...) }
func (s MCPStdioSpec) Environment() map[string]credentialref.Ref {
	result := make(map[string]credentialref.Ref, len(s.environment))
	for name, ref := range s.environment {
		result[name] = ref
	}
	return result
}

type SearchSpec struct {
	provider   string
	endpoint   Endpoint
	credential credentialref.Ref
}

func NewSearchSpec(provider string, endpoint Endpoint, credential credentialref.Ref) (SearchSpec, error) {
	// Live edits and decoded documents must capture the same empty-credential
	// value; a zero Ref must not become a phantom credential after publication.
	credential = credentialref.Parse(credential.String())
	if provider == "" || strings.TrimSpace(provider) != provider || strings.ContainsAny(provider, "\r\n") || endpoint.value == "" {
		return SearchSpec{}, errors.New("search provider and endpoint are required")
	}
	if err := validateCredential(credential); err != nil {
		return SearchSpec{}, err
	}
	if _, err := ParseEndpoint(endpoint.String()); err != nil {
		return SearchSpec{}, err
	}
	return SearchSpec{provider: provider, endpoint: endpoint, credential: credential}, nil
}
func (SearchSpec) Kind() Kind                      { return Search }
func (SearchSpec) isSpec()                         {}
func (s SearchSpec) Provider() string              { return s.provider }
func (s SearchSpec) Endpoint() Endpoint            { return s.endpoint }
func (s SearchSpec) Credential() credentialref.Ref { return s.credential }

// mcpDeclarations retain operator claims used by mount review. Neither a
// successful check nor these declarations establishes verified upstream identity.
// Empty declarations preserve unconfigured Connections; mount admission requires
// both values before granting Workspace tool use.
type mcpDeclarations struct {
	executionIdentity string
	permissionSummary string
}

func newMCPDeclarations(identity, summary string) (mcpDeclarations, error) {
	for _, field := range []struct {
		value string
		limit int
	}{{identity, 256}, {summary, 4096}} {
		if field.value != strings.TrimSpace(field.value) || strings.ContainsAny(field.value, "\r\n") || len(field.value) > field.limit {
			return mcpDeclarations{}, errors.New("invalid MCP operator declarations")
		}
	}
	return mcpDeclarations{identity, summary}, nil
}

// WithOperatorDeclarations returns detached operator claims, never verification.
func (s MCPHTTPSpec) WithOperatorDeclarations(identity, summary string) (MCPHTTPSpec, error) {
	declarations, err := newMCPDeclarations(identity, summary)
	if err != nil {
		return MCPHTTPSpec{}, err
	}
	s.declarations = declarations
	return s, nil
}
func (s MCPHTTPSpec) ExecutionIdentity() string { return s.declarations.executionIdentity }
func (s MCPHTTPSpec) PermissionSummary() string { return s.declarations.permissionSummary }

// WithOperatorDeclarations returns detached operator claims, never verification.
func (s MCPStdioSpec) WithOperatorDeclarations(identity, summary string) (MCPStdioSpec, error) {
	declarations, err := newMCPDeclarations(identity, summary)
	if err != nil {
		return MCPStdioSpec{}, err
	}
	s.declarations = declarations
	return s, nil
}
func (s MCPStdioSpec) ExecutionIdentity() string { return s.declarations.executionIdentity }
func (s MCPStdioSpec) PermissionSummary() string { return s.declarations.permissionSummary }
