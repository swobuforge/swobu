package accessstate

import "github.com/swobuforge/swobu/internal/domain/accessidentity"

// Retained management facts name the admitted actor, independently of the
// affected Principal. Revoking that actor later does not invalidate history.
func validAuditCaller(raw string) bool {
	caller, err := accessidentity.Parse(raw)
	return err == nil && caller.String() == raw
}
