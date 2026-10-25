package credentials

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
	keyringcommodity "github.com/zalando/go-keyring"
)

var keyringDelete = keyringcommodity.Delete

// Connection material is immutable once stored. Serialize adapter instances so
// retries compare before writing; provider/source writers cannot use these slots.
var connectionMaterialMu sync.Mutex

var ErrConnectionMaterialConflict = errors.New("Connection credential slot already contains different material")

// StoreConnectionCredential allocates an independent slot for new material.
// It never accepts a caller-chosen slot or overwrites the current binding.
// Fresh allocation has no uncertain prior write; recovery of a durably
// registered slot uses StoreConnectionCredentialSlot.
// The returned locator belongs only to this Connection scope; publication and
// retirement ordering remain the application transaction owner's responsibility.
func StoreConnectionCredential(id, secret string, policy CredentialWritePolicy) (string, error) {
	scope, err := credentialref.NewConnectionScope(id)
	if err != nil {
		return "", err
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errors.New("cannot allocate Connection credential slot")
	}
	slot := "connections/" + id + "/cred_" + hex.EncodeToString(raw[:])
	ref, err := storeMaterializedCredential(scope, slot, secret, policy)
	if err != nil {
		return "", errors.New("cannot store Connection credential")
	}
	return ref, nil
}

// StoreConnectionCredentialSlot writes an allocated, durably registered slot.
// It is an adapter for the staging transaction: callers must reserve a fresh
// generated slot before calling. Identical retries return its existing locator;
// different material conflicts without writing. Unreadable existing storage
// fails closed because absence has not been proved.
func StoreConnectionCredentialSlot(id, slot, secret string, policy CredentialWritePolicy) (string, error) {
	scope, err := credentialref.NewConnectionScope(id)
	if err != nil || !validConnectionSlot(scope, slot) {
		return "", errors.New("invalid Connection credential slot")
	}
	connectionMaterialMu.Lock()
	defer connectionMaterialMu.Unlock()
	// Inspect only the explicitly selected backend so a lost acknowledgement
	// cannot move an existing slot to another storage authority.
	selected, err := ParseCredentialWritePolicy(string(policy))
	if err != nil {
		return "", err
	}
	backends := []CredentialWritePolicy{selected}
	for _, backend := range backends {
		existing, found, err := existingConnectionMaterial(scope, slot, backend)
		if err != nil {
			// Backend details can contain provider, account, or filesystem
			// information. Preserve fail-closed behavior without projecting them
			// through the credential staging boundary.
			return "", errors.New("cannot inspect existing Connection credential")
		}
		if !found {
			continue
		}
		if !sameConnectionMaterial(existing, secret) {
			return "", ErrConnectionMaterialConflict
		}
		prefix := "secret:"
		if backend == CredentialWritePolicyFile {
			prefix = "secretfile:"
		}
		return prefix + slot, nil
	}
	ref, err := storeMaterializedCredential(scope, slot, secret, policy)
	if err != nil {
		return "", errors.New("cannot store Connection credential")
	}
	return ref, nil
}

func validConnectionSlot(scope credentialref.Scope, slot string) bool {
	_, valid := credentialref.ConnectionSlot(scope, credentialref.Parse("secret:"+slot))
	return valid
}

// RetireConnectionCredential removes only an owned generated slot, after the
// caller has committed replacement and excluded all live references. It never
// resolves secret values, removes external env/files or falls between backends.
// Missing material is already retired and succeeds for recovery retries.
func RetireConnectionCredential(id, ref string) error {
	connectionMaterialMu.Lock()
	defer connectionMaterialMu.Unlock()
	scope, err := credentialref.NewConnectionScope(id)
	if err != nil {
		return err
	}
	locator := credentialref.Parse(ref)
	var name string
	switch locator.Kind() {
	case credentialref.KindSecret:
		name, err = scopedSecretCredentialName(scope, ref)
	case credentialref.KindSecretFile:
		name, err = secretFileCredentialName(ref)
	default:
		return errors.New("credential is not Connection-owned stored material")
	}
	if err != nil || !validConnectionSlot(scope, name) {
		return errors.New("credential is outside Connection scope")
	}
	if locator.Kind() == credentialref.KindSecretFile {
		if err := (&secretFileStore{}).Delete(name); err != nil {
			return errors.New("cannot retire Connection credential")
		}
		return nil
	}
	if err := keyringDelete(KeyringScopeForCredential(scope), name); err != nil && !errors.Is(err, keyringcommodity.ErrNotFound) {
		return errors.New("cannot retire Connection credential")
	}
	return nil
}

// Missing is distinct from unavailable: an unreadable backend cannot authorize
// overwriting a slot whose successful write may have lost its acknowledgement.
func existingConnectionMaterial(scope credentialref.Scope, slot string, policy CredentialWritePolicy) (string, bool, error) {
	if policy == CredentialWritePolicyFile {
		secretFileMu.Lock()
		defer secretFileMu.Unlock()
		document, err := readSecretFileDoc(platformconfig.DefaultAuthCredentialFilePath())
		if err != nil {
			return "", false, err
		}
		value, found := document.Credentials[slot]
		return value, found, nil
	}
	value, err := keyringGet(KeyringScopeForCredential(scope), slot)
	if errors.Is(err, keyringcommodity.ErrNotFound) {
		return "", false, nil
	}
	return value, err == nil, err
}

func sameConnectionMaterial(stored, incoming string) bool {
	decode := func(raw string) (TokenBundle, error) {
		if strings.HasPrefix(strings.TrimSpace(raw), "{") {
			bundle, _, err := DecodeTokenBundle(raw)
			return bundle, err
		}
		return TokenBundle{AccessToken: strings.TrimSpace(raw)}, nil
	}
	left, err := decode(stored)
	if err != nil {
		return false
	}
	right, err := decode(incoming)
	if err != nil {
		return false
	}
	// IssuedAt is adapter metadata regenerated when wrapping a raw token. All
	// execution-relevant material must match, including refresh/ID token and expiry.
	return left.AccessToken != "" && left.AccessToken == right.AccessToken && left.RefreshToken == right.RefreshToken && left.IDToken == right.IDToken && left.ExpiresAt.Equal(right.ExpiresAt)
}

// MaterializeConnectionCredential uses a freshly reserved slot once. Recovery
// must inspect and reuse uncertain prior material.
func MaterializeConnectionCredential(id, slot, secret string, policy CredentialWritePolicy, fresh bool) (string, error) {
	if !fresh {
		return StoreConnectionCredentialSlot(id, slot, secret, policy)
	}
	scope, err := credentialref.NewConnectionScope(id)
	if err != nil || !validConnectionSlot(scope, slot) {
		return "", errors.New("invalid Connection credential slot")
	}
	connectionMaterialMu.Lock()
	defer connectionMaterialMu.Unlock()
	ref, err := storeMaterializedCredential(scope, slot, secret, policy)
	if err != nil {
		return "", errors.New("cannot store Connection credential")
	}
	return ref, nil
}

// ResolveConnectionStoredBundle reads refresh/routing metadata from an exact
// Connection-owned slot. It never changes material or falls between backends.
func ResolveConnectionStoredBundle(id, ref string) (string, error) {
	scope, err := credentialref.NewConnectionScope(id)
	if err != nil {
		return "", err
	}
	locator := credentialref.Parse(ref)
	var slot string
	switch locator.Kind() {
	case credentialref.KindSecret:
		slot, err = scopedSecretCredentialName(scope, ref)
	case credentialref.KindSecretFile:
		slot, err = secretFileCredentialName(ref)
	default:
		return "", errors.New("credential is not Connection-owned stored material")
	}
	if err != nil || !validConnectionSlot(scope, slot) {
		return "", errors.New("credential is outside Connection scope")
	}
	connectionMaterialMu.Lock()
	defer connectionMaterialMu.Unlock()
	policy := CredentialWritePolicyKeyring
	if locator.Kind() == credentialref.KindSecretFile {
		policy = CredentialWritePolicyFile
	}
	raw, found, err := existingConnectionMaterial(scope, slot, policy)
	if err != nil || !found {
		return "", errors.New("cannot resolve Connection credential")
	}
	return raw, nil
}
