package profile

import (
	"strings"

	"slices"
)

// ProviderSetupFieldNamesForSpec returns the operator-facing setup field names
// declared for one provider spec.
func ProviderSetupFieldNamesForSpec(spec string) []string {
	provider, ok := profileFor(spec)
	if !ok || len(provider.SetupFields) == 0 {
		return nil
	}
	return slices.Clone(provider.SetupFields)
}

// ProviderSetupPrimaryFieldForSpec returns the first operator-facing setup
// field declared for one provider spec, or empty if the provider exposes no
// setup fields.
func ProviderSetupPrimaryFieldForSpec(spec string) string {
	fields := ProviderSetupFieldNamesForSpec(spec)
	if len(fields) == 0 {
		return ""
	}
	return strings.TrimSpace(fields[0])
}

// ProviderSetupFieldSummaryForSpec returns a compact inventory string for one
// provider spec.
func ProviderSetupFieldSummaryForSpec(spec string) string {
	fields := ProviderSetupFieldNamesForSpec(spec)
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, ", ")
}
