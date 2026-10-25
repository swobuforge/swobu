package credentialref

import (
	"encoding/hex"
	"strings"
)

// ConnectionSlot names only an immutable generated slot owned by this scope.
// Storage effects and random allocation remain credential-adapter responsibilities.
func ConnectionSlot(scope Scope, ref Ref) (string, bool) {
	if !scope.Valid() || scope.Kind() != ScopeConnection {
		return "", false
	}
	var prefix string
	switch ref.Kind() {
	case KindSecret:
		prefix = "secret:"
	case KindSecretFile:
		prefix = "secretfile:"
	default:
		return "", false
	}
	raw := ref.String()
	if len(raw) < len(prefix) || !strings.EqualFold(raw[:len(prefix)], prefix) {
		return "", false
	}
	slot := strings.TrimSpace(raw[len(prefix):])
	ownedPrefix := "connections/" + scope.Identifier() + "/cred_"
	if !strings.HasPrefix(slot, ownedPrefix) {
		return "", false
	}
	suffix := strings.TrimPrefix(slot, ownedPrefix)
	decoded, err := hex.DecodeString(suffix)
	return slot, err == nil && len(decoded) == 16 && hex.EncodeToString(decoded) == suffix
}
