package profile

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

// BedrockEndpointResolution is the single parsed representation of an
// operator-authored Bedrock endpoint paired with its selected protocol.
type BedrockEndpointResolution struct {
	BaseURL          string
	RequestURL       string
	CatalogURL       string
	InputWasComplete bool
}

// ResolveBedrockEndpoint combines the optional explicit endpoint, authored
// signing region, and selected protocol into one coherent API base, request URL,
// and catalog URL. Before protocol selection, one recognized terminal operation
// is stripped without inferring a protocol. After selection, forgiveness strips
// only that protocol's operation. A conflicting terminal operation, a catalog
// URL, a recognized namespace contradiction, or a canonical AWS
// host/signing-region mismatch is rejected rather than reinterpreted.
func ResolveBedrockEndpoint(explicitEndpoint, region string, kind protocolkind.ProtocolKind) (BedrockEndpointResolution, error) {
	trimmed := strings.TrimSpace(explicitEndpoint)
	if trimmed == "" {
		trimmed = bedrockMantleEndpointForProtocol(region, kind)
	}
	return resolvedBedrock(trimmed, region, kind)
}

// EffectiveBedrockAPIURL projects the normalized API base used for display and
// editor seeding. Invalid explicit input remains visible verbatim so validation
// can explain it without blanking the field.
func EffectiveBedrockAPIURL(region, endpoint string, kind protocolkind.ProtocolKind) string {
	trimmed := strings.TrimSpace(endpoint)
	resolution, err := ResolveBedrockEndpoint(trimmed, region, kind)
	if err != nil || resolution.BaseURL == "" {
		return trimmed
	}
	return resolution.BaseURL
}

// CanonicalBedrockEndpointIntent returns the explicit-or-empty durable intent
// after a valid submission. Submitting the protocol-aware regional default
// restores absent intent; another valid endpoint persists as its normalized API
// base. Invalid input remains visible for the validator to reject.
func CanonicalBedrockEndpointIntent(region, endpoint string, kind protocolkind.ProtocolKind) string {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return ""
	}
	resolution, err := ResolveBedrockEndpoint(trimmed, region, kind)
	if err != nil || resolution.BaseURL == "" {
		return trimmed
	}
	regional, err := ResolveBedrockEndpoint("", region, kind)
	if err == nil && resolution.BaseURL == regional.BaseURL {
		return ""
	}
	return resolution.BaseURL
}

func bedrockMantleEndpointForProtocol(region string, kind protocolkind.ProtocolKind) string {
	normalized := strings.TrimSpace(strings.ToLower(region))
	if normalized == "" {
		return ""
	}
	path := "/v1"
	if kind == protocolkind.Messages {
		path = "/anthropic/v1"
	}
	return "https://bedrock-mantle." + normalized + ".api.aws" + path
}

func resolvedBedrock(raw, region string, kind protocolkind.ProtocolKind) (BedrockEndpointResolution, error) {
	if raw == "" {
		return BedrockEndpointResolution{}, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Hostname() == "" {
		return BedrockEndpointResolution{}, fmt.Errorf("endpoint %q is not a valid absolute URL", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return BedrockEndpointResolution{}, fmt.Errorf("endpoint %q must not include a query or fragment", raw)
	}

	path := strings.TrimRight(u.Path, "/")
	inputWasComplete, err := normalizeBedrockOperation(&path, kind)
	if err != nil {
		return BedrockEndpointResolution{}, fmt.Errorf("endpoint %q %w", raw, err)
	}
	path = strings.TrimRight(path, "/")

	if strings.HasSuffix(path, "/anthropic") {
		path += "/v1"
	}
	hostRegion := BedrockMantleRegionFromEndpoint((&url.URL{Scheme: u.Scheme, Host: u.Host, Path: path}).String())
	if kind != "" {
		classified := classifyBedrockEndpointPath(path)
		if classified != bedrockNamespaceUnrecognized && !namespaceCoherentWithKind(classified, kind) {
			return BedrockEndpointResolution{}, fmt.Errorf("endpoint %q implies a different API namespace than protocol %q", raw, kind)
		}
	}

	baseURL := *u
	baseURL.Path = path
	if hostRegion != "" {
		signingRegion := strings.TrimSpace(strings.ToLower(region))
		if signingRegion != "" && hostRegion != signingRegion {
			return BedrockEndpointResolution{}, fmt.Errorf("endpoint region %s does not match signing region %s", hostRegion, signingRegion)
		}
	}

	requestPath, err := ProviderRequestPath(string(ProviderSpecBedrock), kind)
	if err != nil {
		requestPath = ""
	}
	requestURL := ""
	if requestPath != "" {
		joined := baseURL
		joined.Path = strings.TrimRight(baseURL.Path, "/") + requestPath
		requestURL = joined.String()
	}

	root := catalogRootPath(path)
	catalogURL := ""
	if root != "" {
		catalog := baseURL
		catalog.Path = root + "/models"
		catalogURL = catalog.String()
	}

	return BedrockEndpointResolution{
		BaseURL:          baseURL.String(),
		RequestURL:       requestURL,
		CatalogURL:       catalogURL,
		InputWasComplete: inputWasComplete,
	}, nil
}

func normalizeBedrockOperation(path *string, kind protocolkind.ProtocolKind) (bool, error) {
	operations := []struct {
		kind protocolkind.ProtocolKind
		path string
	}{
		{protocolkind.Responses, "/responses"},
		{protocolkind.Messages, "/messages"},
		{protocolkind.ChatCompletions, "/chat/completions"},
	}
	if strings.HasSuffix(*path, "/models") {
		return false, fmt.Errorf("is a model catalog URL, not an inference endpoint")
	}
	for _, operation := range operations {
		if !strings.HasSuffix(*path, operation.path) {
			continue
		}
		if kind != "" && operation.kind != kind {
			return false, fmt.Errorf("ends in operation %q for a different protocol than %q", operation.path, kind)
		}
		*path = strings.TrimSuffix(*path, operation.path)
		return true, nil
	}
	return false, nil
}

func catalogRootPath(basePath string) string {
	trimmed := strings.TrimRight(basePath, "/")
	if trimmed == "" {
		return "/v1"
	}
	for _, namespace := range []string{"/openai/v1", "/anthropic/v1", "/v1"} {
		prefix := strings.TrimSuffix(trimmed, namespace)
		if prefix != trimmed {
			return strings.TrimRight(prefix, "/") + "/v1"
		}
	}
	return ""
}

func namespaceCoherentWithKind(classified bedrockEndpointNamespaceClass, kind protocolkind.ProtocolKind) bool {
	switch kind {
	case protocolkind.Messages:
		return classified == bedrockNamespaceAnthropic || classified == bedrockNamespaceUnrecognized
	case protocolkind.Responses, protocolkind.ChatCompletions:
		return classified != bedrockNamespaceAnthropic
	default:
		return true
	}
}
