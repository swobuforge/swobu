package connections

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"

	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
)

// CredentialWriter stores material behind an allocated slot, using the configured
// keyring/file policy. It must not log material or reenter the Connection store.
type CredentialWriter func(context.Context, connectionidentity.ID, string, string) (credentialref.Ref, error)

// StageCredential persists both possible backend locators before writing new
// material. The returned locator is internal command input, never browser
// persistence or a read response. Separate staging and binding lets a retried
// binding reuse one immutable locator instead of storing new material again.
// Recovery reclaims abandoned candidates through Lifecycle reconciliation.
func (c *Commands) StageCredential(ctx context.Context, id connectionidentity.ID, secret string, authorize func(context.Context) error, write CredentialWriter) (credentialref.Ref, error) {
	if write == nil || secret == "" {
		return credentialref.Ref{}, errors.New("credential material and storage are required")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return credentialref.Ref{}, errors.New("cannot allocate credential material identity")
	}
	slot := "connections/" + id.String() + "/cred_" + hex.EncodeToString(random[:])
	candidates := []credentialref.Ref{credentialref.Parse("secret:" + slot), credentialref.Parse("secretfile:" + slot)}
	return c.registry.StageCredential(ctx, id, candidates, authorize, func(ctx context.Context) (credentialref.Ref, error) { return write(ctx, id, slot, secret) })
}

// ReplacementWriter owns material equality and fresh-vs-recovery storage policy.
// Material never enters registry intent hashes or public result projections.
var ErrMaterialConflict = errors.New("credential replacement retry contains different material")

type ReplacementWriter func(context.Context, connectionidentity.ID, string, string, bool) (credentialref.Ref, error)

func (c *Commands) ReplaceCredential(ctx context.Context, id connectionidentity.ID, expected domain.Revision, environment, secret string, sameIdentity bool, scope, key string, authorize func(context.Context) error, write ReplacementWriter) (Connection, bool, error) {
	if !sameIdentity {
		return Connection{}, false, ErrIdentityAssertion
	}
	if strings.TrimSpace(secret) == "" || write == nil {
		return Connection{}, false, errors.New("credential material and storage required")
	}
	replacement, err := c.registry.StageCredentialRetry(ctx, id, expected, environment, scope, key, authorize, func() ([]credentialref.Ref, error) {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, errors.New("cannot allocate credential slot")
		}
		slot := "connections/" + id.String() + "/cred_" + hex.EncodeToString(random[:])
		return []credentialref.Ref{credentialref.Parse("secret:" + slot), credentialref.Parse("secretfile:" + slot)}, nil
	}, func(ctx context.Context, candidates []credentialref.Ref, fresh bool) (credentialref.Ref, error) {
		_, slot, _ := strings.Cut(candidates[0].String(), ":")
		return write(ctx, id, slot, secret, fresh)
	})
	if err != nil {
		return Connection{}, false, err
	}
	value, replayed, err := c.registry.ReplaceStagedCredentialChecked(ctx, id, expected, environment, replacement, scope, key, authorize)
	if err != nil {
		return Connection{}, false, err
	}
	result, err := Project(value, nil)
	return result, replayed, err
}
