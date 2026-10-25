package connections

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
)

// CredentialRefs returns distinct locator identities in deterministic order.
// It is an internal configuration projection, never a public API response.
// Resolving secrets or equating their values is deliberately outside this model.
func (c Connection) CredentialRefs() []credentialref.Ref {
	var refs []credentialref.Ref
	switch spec := c.spec.(type) {
	case ModelSpec:
		switch value := spec.upstream.(type) {
		case routing.StandardConnection:
			refs = append(refs, value.Credential())
		case routing.ZAIConnection:
			refs = append(refs, value.Credential())
		case routing.BedrockConnection:
			refs = append(refs, value.Credential())
		case routing.CustomConnection:
			if auth, ok := value.Auth().(routing.CustomHeaderAuth); ok {
				refs = append(refs, auth.Credential())
			}
		}
	case MCPHTTPSpec:
		refs = append(refs, spec.credential)
	case MCPStdioSpec:
		for _, ref := range spec.environment {
			refs = append(refs, ref)
		}
	case SearchSpec:
		refs = append(refs, spec.credential)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].String() < refs[j].String() })
	result := make([]credentialref.Ref, 0, len(refs))
	for _, ref := range refs {
		if ref.Kind() != credentialref.KindEmpty && (len(result) == 0 || result[len(result)-1] != ref) {
			result = append(result, ref)
		}
	}
	return result
}

// ReplaceCredentialBinding publishes a previously stored new locator using the
// same atomic document boundary as configuration edits. The expected revision
// and old binding are rechecked while holding the registry lock. It returns the
// committed value directly, avoiding a racy post-publication readback.
//
// This operation does not store or retire secret material and is not an API
// command receipt. Callers must not overwrite the old secret before calling it.
func (s *Store) ReplaceCredentialBinding(ctx context.Context, id connectionidentity.ID, expected Revision, environment string, expectedRef, replacement credentialref.Ref, now time.Time) (Connection, error) {
	if err := ctx.Err(); err != nil {
		return Connection{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.values[id]
	if !exists {
		return Connection{}, ErrNotFound
	}
	candidate, err := current.ReplaceCredential(expected, environment, expectedRef, replacement, now)
	if err != nil {
		return Connection{}, err
	}
	if err := ctx.Err(); err != nil {
		return Connection{}, err
	}
	values := make(map[connectionidentity.ID]Connection, len(s.values))
	for key, value := range s.values {
		values[key] = value
	}
	values[id] = candidate
	if err := s.publish(values); err != nil {
		return Connection{}, err
	}
	return candidate, nil
}

var ErrCredentialBinding = errors.New("Connection credential binding changed or is absent")

// ReplaceCredential changes only an existing credential binding. expectedRef
// names the inspected locator; environment selects one stdio variable and must
// be empty for every other spec. No endpoint/provider/account fields are accepted
// by this command. The caller must establish that the new credential represents
// the same external identity and store it before publication.
//
// This pure transition neither resolves nor retires credentials. Retirement must
// follow durable publication and account for other users of the old locator.
func (c Connection) ReplaceCredential(expected Revision, environment string, expectedRef, replacement credentialref.Ref, now time.Time) (Connection, error) {
	if c.revision.value == 0 || c.revision != expected {
		return Connection{}, ErrRevisionConflict
	}
	if err := validateCredential(replacement); err != nil || replacement.Kind() == credentialref.KindEmpty {
		return Connection{}, errors.New("validated replacement credential reference is required")
	}
	if expectedRef.Kind() == credentialref.KindEmpty || expectedRef == replacement {
		return Connection{}, ErrCredentialBinding
	}
	var candidate Spec
	var err error
	if _, stdio := c.spec.(MCPStdioSpec); !stdio && environment != "" {
		return Connection{}, ErrCredentialBinding
	}
	switch spec := c.spec.(type) {
	case ModelSpec:
		var upstream routing.Connection
		switch value := spec.upstream.(type) {
		case routing.StandardConnection:
			if value.Credential() != expectedRef {
				return Connection{}, ErrCredentialBinding
			}
			endpoint, _ := value.Locator()
			upstream, err = routing.NewStandardConnection(value.Provider(), endpoint.String(), replacement.String())
		case routing.ZAIConnection:
			if value.Credential() != expectedRef {
				return Connection{}, ErrCredentialBinding
			}
			upstream, err = routing.NewZAIConnection(value.Provider(), value.Access(), replacement.String())
		case routing.BedrockConnection:
			if value.Credential() != expectedRef {
				return Connection{}, ErrCredentialBinding
			}
			upstream, err = routing.NewBedrockConnection(value.Provider(), value.Region(), value.Endpoint(), replacement.String())
		case routing.CustomConnection:
			auth, ok := value.Auth().(routing.CustomHeaderAuth)
			if !ok || auth.Credential() != expectedRef {
				return Connection{}, ErrCredentialBinding
			}
			var changed routing.CustomHeaderAuth
			changed, err = routing.NewCustomHeaderAuth(auth.Name(), replacement.String())
			if err == nil {
				upstream, err = routing.NewCustomConnection(value.Provider(), value.BaseURL().String(), changed)
			}
		default:
			return Connection{}, ErrCredentialBinding
		}
		if err == nil {
			candidate, err = NewModelSpec(upstream)
		}
	case MCPHTTPSpec:
		if spec.credential != expectedRef {
			return Connection{}, ErrCredentialBinding
		}
		changed := spec
		changed.credential = replacement
		candidate = changed
	case MCPStdioSpec:
		if environment == "" || spec.environment[environment] != expectedRef {
			return Connection{}, ErrCredentialBinding
		}
		values := spec.Environment()
		values[environment] = replacement
		changed := spec
		changed.environment = values
		candidate = changed
	case SearchSpec:
		if spec.credential != expectedRef {
			return Connection{}, ErrCredentialBinding
		}
		candidate, err = NewSearchSpec(spec.provider, spec.endpoint, replacement)
	default:
		return Connection{}, ErrCredentialBinding
	}
	if err != nil {
		return Connection{}, errors.New("replacement credential binding is invalid")
	}
	return c.EditSameIdentity(expected, c.label, candidate, now)
}

// CredentialBinding selects one existing binding without resolving its material.
// The returned locator is private command input and must never enter API DTOs.
func (c Connection) CredentialBinding(environment string) (credentialref.Ref, error) {
	if spec, ok := c.spec.(MCPStdioSpec); ok {
		ref, exists := spec.environment[environment]
		if !exists || ref.Kind() == credentialref.KindEmpty {
			return credentialref.Ref{}, ErrCredentialBinding
		}
		return ref, nil
	}
	refs := c.CredentialRefs()
	if environment != "" || len(refs) != 1 {
		return credentialref.Ref{}, ErrCredentialBinding
	}
	return refs[0], nil
}
