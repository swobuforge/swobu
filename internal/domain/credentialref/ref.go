package credentialref

import "strings"

type Kind string

const (
	KindEmpty      Kind = "empty"
	KindKeychain   Kind = "keychain"
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
	case normalized == "keychain" || normalized == "keychain:" || strings.HasPrefix(normalized, "keychain:"):
		return Ref{raw: raw, norm: normalized, kind: KindKeychain}
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
