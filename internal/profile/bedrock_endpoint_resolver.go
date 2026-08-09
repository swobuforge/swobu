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
	InputWasComplete bool
}

// ResolveBedrockEndpoint normalizes one operator-authored inference API URL and
// appends only the selected protocol operation. Region validates canonical AWS
// hosts and supplies signing authority; it never supplies an inference
// namespace. Before protocol selection, one recognized terminal operation is
// stripped without inferring a protocol. A conflicting operation, catalog URL,
// namespace contradiction, or canonical host/region mismatch is rejected.
func ResolveBedrockEndpoint(endpoint, region string, kind protocolkind.ProtocolKind) (BedrockEndpointResolution, error) {
	trimmed := strings.TrimSpace(endpoint)
	if trimmed == "" {
		return BedrockEndpointResolution{}, fmt.Errorf("endpoint is required")
	}
	return resolvedBedrock(trimmed, region, kind)
}

// BedrockCatalogURL returns the canonical regional Mantle model catalog. The
// catalog service is region-owned and independent from a model's authored
// inference namespace.
func BedrockCatalogURL(region string) string {
	normalized := strings.TrimSpace(strings.ToLower(region))
	if normalized == "" {
		return ""
	}
	return "https://bedrock-mantle." + normalized + ".api.aws/v1/models"
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

	return BedrockEndpointResolution{
		BaseURL:          baseURL.String(),
		RequestURL:       requestURL,
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
