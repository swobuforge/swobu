package effect

import (
	"strings"

	platformconfig "github.com/swobuforge/swobu/internal/platform/config"
)

// RoutingModelCatalogLoaded carries routing model catalog choices for one scope.
type RoutingModelCatalogLoaded struct {
	Scope                    string
	ProviderSpec             string
	BaseURL                  string
	CredentialRef            string
	ProviderProtocol         string
	ModelIDs                 []string
	Error                    string
	ResolvedProviderProtocol string
}

// CreateDraftModelProbeCompletedAction carries the result of one real execution
// probe for create-draft readiness.
type CreateDraftModelProbeCompletedAction struct {
	ProviderSpec             string
	BaseURL                  string
	CredentialRef            string
	ModelID                  string
	ProviderProtocol         string
	Error                    string
	ResolvedProviderProtocol string
}

func normalizeOperatorSurfaceError(err error) string {
	message := strings.TrimSpace(err.Error())                                    // swobu:io-string source=boundary
	message = strings.TrimSpace(strings.TrimPrefix(message, "operator client:")) // swobu:io-string source=boundary
	if message == "" {
		return daemonUnavailableHint()
	}
	lower := strings.ToLower(message) // swobu:io-string source=boundary
	if strings.Contains(lower, "is unavailable") ||
		strings.Contains(lower, "request failed") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "deadline exceeded") ||
		strings.Contains(lower, "no such host") {
		return daemonUnavailableHint()
	}
	return message
}

func daemonUnavailableHint() string {
	return "unavailable at " + platformconfig.DefaultDaemonURL()
}

func normalizeModelCatalogProbeLoadError(err error) string {
	normalized := normalizeOperatorSurfaceError(err)
	if strings.Contains(strings.ToLower(normalized), "request timed out") { // swobu:io-string source=boundary
		return "model probe timed out at " + platformconfig.DefaultDaemonURL() + " (retry)"
	}
	return normalized
}
