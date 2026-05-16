package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/swobuforge/swobu/internal/terminalui/apps/cockpit/app/state"
)

const unresolvedProviderIdentifier = "not configured"

func providerHumanIdentifier(pc state.ProviderConfigSnapshot) string {
	alias := strings.TrimSpace(pc.TargetAlias) // trimlowerlint:allow boundary canonicalization
	if alias != "" {
		return alias
	}
	provider := strings.TrimSpace(strings.ToLower(pc.ProviderSpec)) // trimlowerlint:allow boundary canonicalization
	model := strings.TrimSpace(pc.ModelID)                          // trimlowerlint:allow boundary canonicalization
	if provider == "" || model == "" {
		return unresolvedProviderIdentifier
	}
	return provider + ":" + model + ":" + shortStableHash(strings.TrimSpace(pc.CredentialRef)) // trimlowerlint:allow boundary canonicalization
}

func shortStableHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}
