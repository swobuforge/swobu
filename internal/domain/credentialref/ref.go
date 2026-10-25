package credentialref

import (
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
)

// ScopeKind identifies the owner whose credential reference is being resolved.
// Storage kind (secret, secretfile, env, or file) remains a separate decision.
type ScopeKind string

const (
	ScopeConnection ScopeKind = "connection"
	ScopeProvider   ScopeKind = "provider"
)

// Scope binds a credential reference to one runtime owner. Its fields stay
// private so provider and Connection identities cannot be interchanged.
type Scope struct {
	kind       ScopeKind
	identifier string
}

type Kind string

var ErrInvalidScopeSelection = errors.New("credential scope selection is invalid")

// NewProviderScope validates and binds a provider-owned credential reference.
func NewProviderScope(providerSpec string) (Scope, error) {
	providerSpec = strings.TrimSpace(providerSpec)
	if providerSpec == "" || strings.ContainsAny(providerSpec, "\r\n") {
		return Scope{}, errors.New("provider credential scope is required")
	}
	if strings.EqualFold(providerSpec, "mcp") {
		return Scope{}, errors.New("MCP credentials belong to Connections")
	}
	return Scope{kind: ScopeProvider, identifier: providerSpec}, nil
}

// NewConnectionScope binds material to one reusable upstream identity. It is
// distinct from provider ownership even when protocols coincide.
func NewConnectionScope(id string) (Scope, error) {
	if _, err := connectionidentity.Parse(id); err != nil {
		return Scope{}, errors.New("Connection credential scope is invalid")
	}
	return Scope{kind: ScopeConnection, identifier: id}, nil
}

// Kind identifies which credential owner controls the reference.
func (s Scope) Kind() ScopeKind { return s.kind }

// Identifier returns the validated owner identity.
func (s Scope) Identifier() string { return s.identifier }

// Valid reports whether the scope was constructed for a supported owner.
func (s Scope) Valid() bool {
	switch s.kind {
	case ScopeConnection:
		_, err := connectionidentity.Parse(s.identifier)
		return err == nil
	case ScopeProvider:
		return s.identifier != "" && !strings.ContainsAny(s.identifier, "\r\n")
	default:
		return false
	}
}

const (
	KindEmpty      Kind = "empty"
	KindSecret     Kind = "secret"
	KindSecretFile Kind = "secretfile"
	KindEnv        Kind = "env"
	KindFile       Kind = "file"
	KindOther      Kind = "other"
)

type Ref struct {
	raw  string
	norm string
	kind Kind
}

func Parse(raw string) Ref {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=domain
	// swobu:io-string source=boundary for credential-ref prefix classification
	normalized := strings.ToLower(trimmed) // swobu:io-string source=domain
	switch {
	case normalized == "":
		return Ref{raw: raw, norm: normalized, kind: KindEmpty}
	case normalized == "file" || normalized == "file:" || strings.HasPrefix(normalized, "file:") || strings.HasPrefix(normalized, "/") || strings.HasPrefix(normalized, "~/"):
		return Ref{raw: raw, norm: normalized, kind: KindFile}
	case normalized == "env" || normalized == "env:" || strings.HasPrefix(normalized, "env:"):
		return Ref{raw: raw, norm: normalized, kind: KindEnv}
	case normalized == "secretfile" || normalized == "secretfile:" || strings.HasPrefix(normalized, "secretfile:"):
		return Ref{raw: raw, norm: normalized, kind: KindSecretFile}
	case normalized == "secret" || normalized == "secret:" || strings.HasPrefix(normalized, "secret:"):
		return Ref{raw: raw, norm: normalized, kind: KindSecret}
	default:
		return Ref{raw: raw, norm: normalized, kind: KindOther}
	}
}

func (r Ref) Kind() Kind {
	return r.kind
}

func (r Ref) String() string {
	return strings.TrimSpace(r.raw) // swobu:io-string source=domain
}

func (r Ref) IsFileRef() bool {
	return r.kind == KindFile
}

func (r Ref) IsEmptyFileSelection() bool {
	if r.kind != KindFile {
		return false
	}
	if r.norm == "file" || r.norm == "file:" {
		return true
	}
	if strings.HasPrefix(r.norm, "file:") {
		return strings.TrimSpace(strings.TrimPrefix(r.norm, "file:")) == "" // swobu:io-string source=domain
	}
	return false
}
