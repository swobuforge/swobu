package profile

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	azureResourceSuffix = ".services.ai.azure.com"
	azureOpenAISuffix   = ".openai.azure.com"
	azureProjectMarker  = "/api/projects/"
)

func NormalizeAzureResourceLocator(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
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
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return "", fmt.Errorf("azure resource locator is invalid: missing host")
	}
	if resource := strings.TrimSuffix(host, azureResourceSuffix); resource != host && resource != "" {
		return azureResourceURL(resource), nil
	}
	if resource := strings.TrimSuffix(host, azureOpenAISuffix); resource != host && resource != "" {
		return azureResourceURL(resource), nil
	}
	if !strings.Contains(trimmed, "://") {
		return azureResourceURL(trimmed), nil
	}
	return "", fmt.Errorf("azure resource locator %q could not be normalized", raw)
}
func NormalizeAzureProjectEndpoint(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
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
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return "", fmt.Errorf("azure project endpoint is invalid: missing host")
	}
	if resource := strings.TrimSuffix(host, azureOpenAISuffix); resource != host && resource != "" {
		return "", fmt.Errorf("not a project endpoint")
	}
	resource := strings.TrimSuffix(host, azureResourceSuffix)
	if resource == host || resource == "" {
		return "", fmt.Errorf("not an Azure AI project endpoint")
	}
	path := strings.TrimRight(parsed.EscapedPath(), "/")
	index := strings.Index(strings.ToLower(path), azureProjectMarker)
	if index < 0 {
		return "", fmt.Errorf("not a project endpoint")
	}
	project := strings.Trim(path[index+len(azureProjectMarker):], "/")
	if project == "" || strings.Contains(project, "/") {
		return "", fmt.Errorf("not a project endpoint")
	}
	parsed.Scheme = "https"
	parsed.Host = host
	parsed.Path = azureProjectMarker + project
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}
func AzureResourceRootFromProjectEndpoint(raw string) (string, error) {
	project, err := NormalizeAzureProjectEndpoint(raw)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(project)
	if err != nil {
		return "", err
	}
	resource := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), azureResourceSuffix)
	if resource == "" {
		return "", fmt.Errorf("not an Azure AI project endpoint")
	}
	return azureResourceURL(resource), nil
}
func azureResourceURL(resource string) string {
	return "https://" + strings.TrimSpace(resource) + azureResourceSuffix
}
