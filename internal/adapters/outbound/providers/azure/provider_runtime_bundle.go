package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/swobuforge/swobu/internal/adapters/outbound/httpedge"
	anthropicprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/anthropic"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/protocolcodec"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/carrier"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/provider"
)

// FIXME swobuCallerUAHeaderValue must be DRY across all providers. Consider centralizing if more providers need it.
const swobuCallerUAHeaderValue = "swobu/dev"
const azureDeploymentListPath = "/deployments?api-version=v1&deploymentType=ModelDeployment"

type azureBackendAdapter struct {
	openAI    openaifamily.BackendAdapter
	anthropic anthropicprovider.BackendAdapter
}

type azureProviderModelCatalogClient struct {
	client      *http.Client
	credentials providersruntime.CredentialProvider
}

type azureDeploymentDocument struct {
	Name           string                          `json:"name"`
	Type           string                          `json:"type"`
	ModelName      string                          `json:"modelName"`
	ModelVersion   string                          `json:"modelVersion"`
	ModelPublisher string                          `json:"modelPublisher"`
	Capabilities   azureDeploymentCapabilitiesJSON `json:"facts"`
	Sku            azureDeploymentSkuDocument      `json:"sku"`
}

type azureDeploymentPageResponse struct {
	Data          []azureDeploymentDocument `json:"data"`
	Value         []azureDeploymentDocument `json:"value"`
	NextLink      string                    `json:"nextLink"`
	ODataNextLink string                    `json:"@odata.nextLink"`
}

type azureDeploymentSkuDocument struct {
	Name     string `json:"name"`
	Tier     string `json:"tier,omitempty"`
	Capacity int    `json:"capacity,omitempty"`
}

type azureDeploymentCapabilitiesJSON map[string]json.RawMessage

type azureDeploymentCapabilityRecord struct {
	ChatCompletion bool
	Completion     bool
	Messages       bool
}

// NewRuntime builds the Azure provider runtime by routing concrete protocol
// families to the shared OpenAI-compatible or Anthropic executors.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider) providersruntime.ProviderRuntimeBundle {
	if client == nil {
		client = http.DefaultClient
	}
	router := azureBackendAdapter{
		openAI:    openaifamily.NewExecutor(client, credentials, NewPolicy()),
		anthropic: anthropicprovider.NewExecutor(client, credentials),
	}
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         profile.ProviderSpecAzure,
		BackendResolver:    router,
		CredentialProvider: credentials,
		Discovery: azureProviderModelCatalogClient{
			client:      client,
			credentials: credentials,
		},
	}
}

func (r azureBackendAdapter) ResolveBackend(target provider.TargetSnapshot) (provider.Backend, error) {
	_, err := resolveAzureResourceRoot(target.BaseURL)
	if err != nil {
		return provider.Backend{}, canonical.BadEndpoint("azure resource locator is required")
	}
	codec := protocolcodec.Codec{Protocol: target.ProtocolKind}
	backend := provider.Backend{Target: target.Clone(), Codec: codec, Transport: provider.BindTransport(target, r.Send)}
	if err := backend.Validate(); err != nil {
		return provider.Backend{}, err
	}
	return backend, nil
}

func (r azureBackendAdapter) Send(ctx context.Context, target provider.TargetSnapshot, doc carrier.Document) (provider.Ingress, error) {
	baseURL, err := resolveAzureResourceRoot(target.BaseURL)
	if err != nil {
		return nil, provider.AttemptNotDispatched(canonical.BadEndpoint("azure resource locator is required"))
	}
	next := target.Clone()
	switch target.ProtocolKind {
	case protocolkind.Messages:
		next.BaseURL = strings.TrimRight(baseURL, "/") + "/anthropic/v1"
		return r.anthropic.Send(ctx, next, doc)
	default:
		next.BaseURL = strings.TrimRight(baseURL, "/") + "/openai/v1"
		return r.openAI.Send(ctx, next, doc)
	}
}

var _ provider.BackendResolver = azureBackendAdapter{}

func (c azureProviderModelCatalogClient) ListDeployments(ctx context.Context, target provider.TargetSnapshot) ([]profile.ProviderDeploymentRecord, error) {
	projectEndpoint, err := resolveAzureProjectEndpoint(target.BaseURL)
	if err != nil {
		return nil, canonical.BadEndpoint("azure project endpoint is required")
	}
	// Azure project discovery is scoped to the exact project endpoint. Keep the
	// /api/projects/<project> prefix intact instead of collapsing to the
	// resource root here.
	nextURL := strings.TrimRight(projectEndpoint, "/") + azureDeploymentListPath
	out := make([]profile.ProviderDeploymentRecord, 0, 16)
	for nextURL != "" {
		deployments, nextLink, err := c.listDeploymentsPage(ctx, target, nextURL)
		if err != nil {
			return nil, err
		}
		out = append(out, deployments...)
		if strings.TrimSpace(nextLink) == "" { // swobu:io-string source=boundary
			break
		}
		nextURL = resolveAzureNextLink(projectEndpoint, nextLink)
	}
	return out, nil
}

func (c azureProviderModelCatalogClient) ProbeTarget(ctx context.Context, target provider.TargetSnapshot) (provider.TargetProbeResult, error) {
	deployments, err := c.ListDeployments(ctx, target)
	return provider.TargetProbeResult{Deployments: deployments}, err
}

func (c azureProviderModelCatalogClient) listDeploymentsPage(ctx context.Context, target provider.TargetSnapshot, requestURL string) ([]profile.ProviderDeploymentRecord, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, "", canonical.BadEndpoint("azure provider deployment inventory request could not be built")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip, deflate, zstd")
	req.Header.Set("User-Agent", swobuCallerUAHeaderValue)
	if err := c.applyCredential(ctx, req, target); err != nil {
		return nil, "", err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", canonical.BadEndpoint("azure provider deployment inventory request failed before backend response")
	}
	decodedResp, err := httpedge.DecodeHTTPResponseContentEncoding(resp)
	if err != nil {
		defer func() { _ = resp.Body.Close() }()
		return nil, "", canonical.InternalError("backend response content encoding is unsupported or invalid")
	}
	resp = decodedResp
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, "", httpedge.ReadBackendHTTPError(resp, target.TargetID)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", canonical.InternalError("backend deployment inventory could not be read")
	}
	documents, nextLink, err := decodeAzureDeploymentDocuments(raw)
	if err != nil {
		return nil, "", canonical.InternalError("backend deployment inventory could not be decoded")
	}
	deployments := make([]profile.ProviderDeploymentRecord, 0, len(documents))
	for _, doc := range documents {
		if dep, ok := azureDeploymentDocumentToDeployment(doc); ok {
			deployments = append(deployments, dep)
		}
	}
	return deployments, nextLink, nil
}

func (c azureProviderModelCatalogClient) applyCredential(ctx context.Context, req *http.Request, target provider.TargetSnapshot) error {
	if strings.TrimSpace(target.CredentialRef) == "" { // swobu:io-string source=boundary
		return canonical.BadEndpoint("azure provider credential reference is required")
	}
	if c.credentials == nil {
		return canonical.BadEndpoint("credential resolver is not configured")
	}
	token, err := c.credentials.ResolveCredential(ctx, target.ProviderID(), target.CredentialRef)
	if err != nil {
		return canonical.BadEndpoint("credential reference could not be resolved")
	}
	if strings.TrimSpace(token) == "" { // swobu:io-string source=boundary
		return canonical.BadEndpoint("credential reference resolved to an empty token")
	}
	auth := openaifamily.AuthStrategyForHeader(target.AuthHeader, NewPolicy().AuthStrategy())
	auth.Apply(req, token)
	return nil
}

func decodeAzureDeploymentDocuments(raw []byte) ([]azureDeploymentDocument, string, error) {
	var array []azureDeploymentDocument
	if err := json.Unmarshal(raw, &array); err == nil {
		return array, "", nil
	}
	var page azureDeploymentPageResponse
	if err := json.Unmarshal(raw, &page); err == nil {
		if len(page.Data) > 0 {
			return page.Data, strings.TrimSpace(page.NextLink), nil // swobu:io-string source=boundary
		}
		if len(page.Value) > 0 {
			return page.Value, strings.TrimSpace(page.NextLink), nil // swobu:io-string source=boundary
		}
		nextLink := strings.TrimSpace(page.NextLink) // swobu:io-string source=boundary
		if nextLink == "" {
			nextLink = strings.TrimSpace(page.ODataNextLink) // swobu:io-string source=boundary
		}
		return []azureDeploymentDocument{}, nextLink, nil
	}
	return nil, "", fmt.Errorf("azure deployment inventory payload was not a deployment array or page")
}

func azureDeploymentDocumentToDeployment(doc azureDeploymentDocument) (profile.ProviderDeploymentRecord, bool) {
	name := azureDeploymentName(doc)
	if name == "" {
		return profile.ProviderDeploymentRecord{}, false
	}
	modelName := strings.TrimSpace(doc.ModelName) // swobu:io-string source=boundary
	if modelName == "" {
		modelName = name
	}
	family := deploymentFamilyForDeployment(doc.ModelPublisher)
	supportedProtocols := supportedProviderProtocolsForDeployment(doc.ModelPublisher)
	defaultProtocol := defaultProviderProtocolForDeployment(doc.ModelPublisher)
	if defaultProtocol == "" && len(supportedProtocols) > 0 {
		defaultProtocol = supportedProtocols[0]
	}
	return profile.NewProviderDeployment(
		name,
		modelName,
		doc.ModelPublisher,
		strings.TrimSpace(doc.ModelVersion), // swobu:io-string source=boundary
		family,
		supportedProtocols,
		defaultProtocol,
	), true
}

func azureDeploymentName(doc azureDeploymentDocument) string {
	for _, candidate := range []string{
		doc.Name,
		doc.ModelName,
	} {
		candidate = strings.TrimSpace(candidate) // swobu:io-string source=boundary
		if candidate == "" {
			continue
		}
		if idx := strings.LastIndex(candidate, "/"); idx >= 0 {
			candidate = strings.TrimSpace(candidate[idx+1:]) // swobu:io-string source=boundary
		}
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func resolveAzureProjectEndpoint(raw string) (string, error) {
	candidate := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if candidate == "" {
		return "", fmt.Errorf("azure project endpoint is required")
	}
	if !strings.Contains(candidate, "://") {
		candidate = "https://" + candidate
	}
	parsed, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("azure project endpoint is invalid: %w", err)
	}
	if parsed.Hostname() == "" || parsed.Scheme == "" {
		return "", fmt.Errorf("azure project endpoint is invalid")
	}
	if !strings.Contains(strings.ToLower(strings.TrimSpace(parsed.Path)), "/api/projects/") { // swobu:io-string source=boundary
		return "", fmt.Errorf("azure project endpoint must include /api/projects/<project>")
	}
	parsed.Fragment = ""
	parsed.RawQuery = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (c azureDeploymentCapabilitiesJSON) facts() azureDeploymentCapabilityRecord {
	return azureDeploymentCapabilityRecord{
		ChatCompletion: c.bool("chat_completion") || c.bool("inference"),
		Completion:     c.bool("completion"),
		Messages:       c.bool("messages"),
	}
}

func (c azureDeploymentCapabilitiesJSON) bool(key string) bool {
	if len(c) == 0 {
		return false
	}
	raw, ok := c[key]
	if !ok {
		return false
	}
	var asBool bool
	if err := json.Unmarshal(raw, &asBool); err == nil {
		return asBool
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.EqualFold(strings.TrimSpace(asString), "true") // swobu:io-string source=boundary
	}
	return false
}

func defaultProviderProtocolForDeployment(modelPublisher string) string {
	if deploymentFamilyForDeployment(modelPublisher) == azureDeploymentFamilyAnthropic {
		return azureSupportedProviderProtocolsAnthropic[0]
	}
	return azureSupportedProviderProtocolsOpenAI[0]
}

func resolveAzureResourceRoot(raw string) (string, error) {
	candidate := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if candidate == "" {
		return "", fmt.Errorf("azure resource locator is required")
	}
	if root, err := profile.AzureResourceRootFromProjectEndpoint(candidate); err == nil {
		return root, nil
	}
	return profile.NormalizeAzureResourceLocator(candidate)
}

func resolveAzureNextLink(resourceRoot string, nextLink string) string {
	root := strings.TrimSpace(resourceRoot) // swobu:io-string source=boundary
	nextLink = strings.TrimSpace(nextLink)
	if nextLink == "" {
		return ""
	}
	if strings.Contains(nextLink, "://") {
		return nextLink
	}
	baseURL, err := url.Parse(strings.TrimRight(root, "/") + "/")
	if err != nil {
		return nextLink
	}
	relative, err := url.Parse(nextLink)
	if err != nil {
		return nextLink
	}
	return baseURL.ResolveReference(relative).String()
}
