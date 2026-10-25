package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	credentials "github.com/swobuforge/swobu/internal/adapters/outbound/credentials"
	"github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/provider"
)

// publishRefreshedConnection stages fresh immutable material before the registry
// CAS. A losing edit/delete race leaves a leased slot for ordinary reconciliation;
// it never changes the captured slot or adopts an unrelated current revision.
func publishRefreshedConnection(ctx context.Context, registry *connections.Store, target provider.TargetSnapshot, material string, policy credentials.CredentialWritePolicy) error {
	id, revision, bound := target.ConnectionIdentity()
	if !bound || material == "" {
		return errors.New("Connection refresh requires captured identity and material")
	}
	expected, err := connections.ParseRevision(strconv.FormatUint(revision, 10))
	if err != nil {
		return err
	}
	current, err := registry.Get(id)
	if err != nil {
		return err
	}
	if current.Revision() != expected {
		return connections.ErrRevisionConflict
	}
	old := credentialref.Parse(target.CredentialRef)
	if !connections.OwnedCredentialSlot(id, old) {
		return connections.ErrCredentialBinding
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return errors.New("cannot allocate Connection refresh slot")
	}
	slot := "connections/" + id.String() + "/cred_" + hex.EncodeToString(random[:])
	candidates := []credentialref.Ref{credentialref.Parse("secret:" + slot), credentialref.Parse("secretfile:" + slot)}
	replacement, err := registry.StageCredential(ctx, id, candidates, func(ctx context.Context) error { return ctx.Err() }, func(ctx context.Context) (credentialref.Ref, error) {
		if err := ctx.Err(); err != nil {
			return credentialref.Ref{}, err
		}
		raw, err := credentials.MaterializeConnectionCredential(id.String(), slot, material, policy, true)
		return credentialref.Parse(raw), err
	})
	if err != nil {
		return err
	}
	_, err = registry.ReplaceCredentialBinding(ctx, id, expected, "", old, replacement, time.Now().UTC())
	return err
}
