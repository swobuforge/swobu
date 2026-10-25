package connections

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
)

var ErrCredentialRetired = errors.New("credential binding has been retired")
var ErrCredentialInUse = errors.New("credential is still referenced")

// OwnedCredentialSlot recognizes only immutable material belonging to this
// Connection. External references and provider/source material never qualify for
// retirement here, regardless of whether they currently have consumers.
func OwnedCredentialSlot(id connectionidentity.ID, ref credentialref.Ref) bool {
	if id.String() == "" || (ref.Kind() != credentialref.KindSecret && ref.Kind() != credentialref.KindSecretFile) {
		return false
	}
	_, name, _ := strings.Cut(ref.String(), ":")
	prefix := "connections/" + id.String() + "/cred_"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(name, prefix)
	raw, err := hex.DecodeString(suffix)
	return err == nil && len(raw) == 16 && hex.EncodeToString(raw) == suffix
}

func validateRetirements(values map[connectionidentity.ID]Connection, retired map[string]string) error {
	for ref, owner := range retired {
		id, err := connectionidentity.Parse(owner)
		if err != nil || !OwnedCredentialSlot(id, credentialref.Parse(ref)) {
			return errors.New("invalid retired Connection credential")
		}
	}
	for _, value := range values {
		for _, ref := range value.CredentialRefs() {
			if _, exists := retired[ref.String()]; exists {
				return ErrCredentialRetired
			}
		}
	}
	return nil
}

func usesRef(value Connection, ref credentialref.Ref) bool {
	for _, candidate := range value.CredentialRefs() {
		if candidate.String() == ref.String() {
			return true
		}
	}
	return false
}

// RetireCredentialIfUnused checks current configuration while holding the shared
// Workspace-then-Connection publication locks. A durable retirement marker denies
// future rebinding before the storage callback runs. Storage failure leaves the
// marker for retry; missing material must be idempotent at the storage edge.
// The callback cannot reenter either store. It receives no secret value.
// Retained retry snapshots are historical results, not live credential users.
func (l *Lifecycle) RetireCredentialIfUnused(ctx context.Context, id connectionidentity.ID, ref credentialref.Ref, retire func(context.Context, connectionidentity.ID, credentialref.Ref) error) error {
	if !OwnedCredentialSlot(id, ref) || retire == nil {
		return errors.New("owned Connection slot and retirement adapter are required")
	}
	return l.workspaces.InspectLocked(ctx, func(config routing.Config) error {
		l.registry.mu.Lock()
		defer l.registry.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, value := range l.registry.values {
			if usesRef(value, ref) {
				return ErrCredentialInUse
			}
		}
		if _, marked := l.registry.retired[ref.String()]; !marked {
			retired := make(map[string]string, len(l.registry.retired)+1)
			for key, value := range l.registry.retired {
				retired[key] = value
			}
			retired[ref.String()] = id.String()
			if err := l.registry.publishRetirements(retired); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return retire(ctx, id, ref)
	})
}
