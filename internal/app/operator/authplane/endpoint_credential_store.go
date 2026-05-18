package authplane

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	operatorendpoints "github.com/swobuforge/swobu/internal/app/operator/endpoints"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
)

const endpointRefDelimiter = "#"
const subjectRefPrefix = "subject:"

// EncodeEndpointCredentialLocator returns the canonical endpoint locator used by
// authplane persistence: <endpoint-name>#<provider-config-ref>.
func EncodeEndpointCredentialLocator(endpointName string, providerRef string) string {
	return strings.TrimSpace(endpointName) + endpointRefDelimiter + strings.TrimSpace(providerRef) // swobu:io-string source=boundary
}

// EndpointCredentialStore persists resolved credential refs into endpoint
// intent provider configs.
type EndpointCredentialStore struct {
	endpoints operatorendpoints.OperatorEndpointStore
}

func NewEndpointCredentialStore(endpoints operatorendpoints.OperatorEndpointStore) EndpointCredentialStore {
	return EndpointCredentialStore{endpoints: endpoints}
}

func (s EndpointCredentialStore) UpsertCredentialRef(ctx context.Context, providerSpec string, endpointRef string, credentialRef string) (string, error) {
	slog.Debug("auth credential upsert requested",
		"component", "authplane",
		"provider_spec", strings.TrimSpace(strings.ToLower(providerSpec)), // swobu:io-string source=boundary
		"endpoint_ref_kind", endpointRefKind(endpointRef),
	)
	if isTransientSubjectLocator(endpointRef) {
		// Draft-scoped auth subjects are pre-create and intentionally do not
		// mutate endpoint intent yet. The caller materializes this ref on create.
		return strings.TrimSpace(credentialRef), nil // swobu:io-string source=boundary
	}
	endpointNameRaw, providerRefRaw, err := decodeEndpointCredentialLocator(endpointRef)
	if err != nil {
		return "", err
	}
	endpointName, err := endpointintent.ParseEndpointName(endpointNameRaw)
	if err != nil {
		return "", err
	}
	providerRef, err := endpointintent.ParseProviderConfigRef(providerRefRaw)
	if err != nil {
		return "", err
	}

	ep, err := s.endpoints.Get(ctx, endpointName.String())
	if err != nil {
		slog.Warn("auth credential upsert endpoint lookup failed",
			"component", "authplane",
			"endpoint_name", endpointName.String(),
			"error", err.Error(),
		)
		return "", err
	}
	configs := ep.ProviderConfigs()
	updated := false
	for i := range configs {
		if configs[i].Ref() != providerRef {
			continue
		}
		next, err := cloneProviderConfigWithCredentialRef(configs[i], providerSpec, credentialRef)
		if err != nil {
			return "", err
		}
		configs[i] = next
		updated = true
		break
	}
	if !updated {
		slog.Warn("auth credential upsert provider ref unresolved",
			"component", "authplane",
			"endpoint_name", endpointName.String(),
			"provider_ref", providerRef.String(),
		)
		return "", fmt.Errorf("provider config ref %q is unresolved in endpoint %q", providerRef.String(), endpointName.String())
	}
	updatedEndpoint, err := endpointintent.NewEndpoint(ep.Name(), configs, ep.SelectedProviderConfigRef())
	if err != nil {
		return "", err
	}
	if _, err := s.endpoints.Put(ctx, updatedEndpoint); err != nil {
		slog.Warn("auth credential upsert endpoint write failed",
			"component", "authplane",
			"endpoint_name", endpointName.String(),
			"error", err.Error(),
		)
		return "", err
	}
	slog.Debug("auth credential upsert completed",
		"component", "authplane",
		"endpoint_name", endpointName.String(),
		"provider_ref", providerRef.String(),
	)
	return strings.TrimSpace(credentialRef), nil // swobu:io-string source=boundary
}

func endpointRefKind(raw string) string {
	if isTransientSubjectLocator(raw) {
		return "transient_subject"
	}
	return "endpoint_locator"
}

func isTransientSubjectLocator(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), subjectRefPrefix) // swobu:io-string source=boundary
}

func decodeEndpointCredentialLocator(raw string) (endpointName string, providerRef string, err error) {
	locator := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if locator == "" {
		return "", "", fmt.Errorf("endpoint ref is required")
	}
	parts := strings.SplitN(locator, endpointRefDelimiter, 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("endpoint ref must use %q separator", endpointRefDelimiter)
	}
	endpointName = strings.TrimSpace(parts[0]) // swobu:io-string source=boundary
	providerRef = strings.TrimSpace(parts[1])  // swobu:io-string source=boundary
	if endpointName == "" || providerRef == "" {
		return "", "", fmt.Errorf("endpoint ref must include endpoint name and provider ref")
	}
	return endpointName, providerRef, nil
}

func cloneProviderConfigWithCredentialRef(cfg endpointintent.ProviderConfig, providerSpec string, credentialRef string) (endpointintent.ProviderConfig, error) {
	currentSpec := strings.TrimSpace(cfg.ProviderSpec().String())                                                     // swobu:io-string source=boundary
	if spec := strings.TrimSpace(strings.ToLower(providerSpec)); spec != "" && spec != strings.ToLower(currentSpec) { // swobu:io-string source=boundary
		return endpointintent.ProviderConfig{}, fmt.Errorf("provider spec mismatch for credential persistence")
	}
	next, err := endpointintent.NewProviderConfig(
		cfg.Ref(),
		cfg.ProviderSpec(),
		cfg.BaseURL(),
		strings.TrimSpace(credentialRef), // swobu:io-string source=boundary
	)
	if err != nil {
		return endpointintent.ProviderConfig{}, err
	}
	next, err = next.WithModelID(cfg.ModelID())
	if err != nil {
		return endpointintent.ProviderConfig{}, err
	}
	next, err = next.WithSelectedFrame(cfg.SelectedFrame())
	if err != nil {
		return endpointintent.ProviderConfig{}, err
	}
	next, err = next.WithTargetAlias(cfg.TargetAlias())
	if err != nil {
		return endpointintent.ProviderConfig{}, err
	}
	return next, nil
}
