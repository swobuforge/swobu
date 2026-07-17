package endpointintent

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	azureResourceLocatorSuffix       = ".services.ai.azure.com"
	azureOpenAIResourceLocatorSuffix = ".openai.azure.com"
	azureProjectPathMarker           = "/api/projects/"
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

// NormalizeAzureProjectEndpoint canonicalizes the Azure AI Foundry project
// endpoint used for deployment inventory. Unlike execution endpoints, this
// locator intentionally preserves /api/projects/<project>.
func NormalizeAzureProjectEndpoint(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if trimmed == "" {
		return "", fmt.Errorf("azure project endpoint is required")
	}
	candidate := trimmed
	if !strings.Contains(candidate, "://") {
		candidate = "https://" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("azure project endpoint is invalid: %w", err)
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname())) // swobu:io-string source=boundary
	if host == "" {
		return "", fmt.Errorf("azure project endpoint is invalid: missing host")
	}
	if resource := strings.TrimSuffix(host, azureOpenAIResourceLocatorSuffix); resource != host && resource != "" {
		return "", fmt.Errorf("not a project endpoint")
	}
	resource := strings.TrimSuffix(host, azureResourceLocatorSuffix)
	if resource == host || resource == "" {
		return "", fmt.Errorf("not an Azure AI project endpoint")
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	lowerPath := strings.ToLower(path)
	idx := strings.Index(lowerPath, azureProjectPathMarker)
	if idx < 0 {
		return "", fmt.Errorf("not a project endpoint")
	}
	project := strings.Trim(path[idx+len(azureProjectPathMarker):], "/")
	if project == "" || strings.Contains(project, "/") {
		return "", fmt.Errorf("not a project endpoint")
	}
	parsed.Scheme = "https"
	parsed.Host = host
	parsed.Path = azureProjectPathMarker + project
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

// AzureResourceRootFromProjectEndpoint derives the execution resource root from
// a project endpoint without making that derived URL user intent.
func AzureResourceRootFromProjectEndpoint(raw string) (string, error) {
	projectEndpoint, err := NormalizeAzureProjectEndpoint(raw)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(projectEndpoint)
	if err != nil {
		return "", err
	}
	resource := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), azureResourceLocatorSuffix)
	if resource == "" {
		return "", fmt.Errorf("not an Azure AI project endpoint")
	}
	return azureResourceLocatorURL(resource), nil
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
