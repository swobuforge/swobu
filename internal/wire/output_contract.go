package wire

import (
	"strings"

	"github.com/swobuforge/swobu/internal/domain/canonical"
)

// RejectUnknownOutputFormat rejects a discriminator outside a protocol's
// documented closed output-format union. The singleton contract cannot erase
// into unconstrained output or degrade at an additive child boundary.
func RejectUnknownOutputFormat(protocol, contract string) error {
	return canonical.BadRequest(
		strings.TrimSpace(protocol) + " output contract is invalid: " + strings.TrimSpace(contract),
	)
}
