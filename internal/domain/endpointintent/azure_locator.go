package endpointintent

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	azureResourceLocatorSuffix       = ".services.ai.azure.com"
	azureOpenAIResourceLocatorSuffix = ".openai.azure.com"
)

// NormalizeAzureResourceLocator canonicalizes a user-entered Azure resource
// locator to the resource-root host that the runtime edges can compose from.
func NormalizeAzureResourceLocator(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if trimmed == "" {
		return "", fmt.Errorf("azure resource locator is required")
	}
	candidate := trimmed
	if !strings.Contains(candidate, "://") {
		candidate = "https://" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("azure resource locator is invalid: %w", err)
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname())) // swobu:io-string source=boundary
	if host == "" {
		return "", fmt.Errorf("azure resource locator is invalid: missing host")
	}
	if resource := strings.TrimSuffix(host, azureResourceLocatorSuffix); resource != host && resource != "" {
		return azureResourceLocatorURL(resource), nil
	}
	if resource := strings.TrimSuffix(host, azureOpenAIResourceLocatorSuffix); resource != host && resource != "" {
		return azureResourceLocatorURL(resource), nil
	}
	if resource, ok := azureResourceNameFromPortalLocator(parsed); ok {
		return azureResourceLocatorURL(resource), nil
	}
	if !strings.Contains(trimmed, "://") {
		resource := strings.TrimSpace(trimmed) // swobu:io-string source=boundary
		if resource == "" {
			return "", fmt.Errorf("azure resource locator is required")
		}
		return azureResourceLocatorURL(resource), nil
	}
	return "", fmt.Errorf("azure resource locator %q could not be normalized to a resource root", raw)
}

func azureResourceLocatorURL(resource string) string {
	return "https://" + strings.TrimSpace(resource) + azureResourceLocatorSuffix
}

func azureResourceNameFromPortalLocator(parsed *url.URL) (string, bool) {
	if parsed == nil {
		return "", false
	}
	candidates := []string{parsed.Path, parsed.Fragment}
	for _, candidate := range candidates {
		trimmed := strings.Trim(candidate, "/") // swobu:io-string source=boundary
		if trimmed == "" {
			continue
		}
		parts := strings.Split(trimmed, "/")
		for idx, part := range parts {
			if idx+1 >= len(parts) {
				continue
			}
			part = strings.ToLower(strings.TrimSpace(part)) // swobu:io-string source=boundary
			if part != "accounts" && part != "workspaces" {
				continue
			}
			resource := strings.TrimSpace(parts[idx+1]) // swobu:io-string source=boundary
			if resource != "" {
				return resource, true
			}
		}
	}
	return "", false
}
