package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	anthropicprovider "github.com/swobuforge/swobu/internal/adapters/outbound/providers/anthropic"
	"github.com/swobuforge/swobu/internal/adapters/outbound/providers/openaifamily"
	providersruntime "github.com/swobuforge/swobu/internal/adapters/outbound/providers/runtime"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/domain/endpointintent"
	"github.com/swobuforge/swobu/internal/domain/protocolkind"
	"github.com/swobuforge/swobu/internal/exchange"
	"github.com/swobuforge/swobu/internal/profile"
)

// FIXME swobuCallerUAHeaderValue must be DRY across all providers. Consider centralizing if more providers need it.
const swobuCallerUAHeaderValue = "swobu/dev"
const azureDeploymentListPath = "/deployments?api-version=v1&deploymentType=ModelDeployment"

type azureProviderIngressResolver struct {
	openAI          openaifamily.ProviderIngressResolverAdapter
	anthropic       anthropicprovider.ProviderIngressResolverAdapter
	projectEndpoint string
}

type authHeaderMappingRoundTripper struct {
	base http.RoundTripper
	from string
	to   string
}

type azureProviderModelCatalogClient struct {
	client          *http.Client
	credentials     providersruntime.CredentialProvider
	projectEndpoint string
}

type azureDeploymentDocument struct {
	Name           string                          `json:"name"`
	Type           string                          `json:"type"`
	ModelName      string                          `json:"modelName"`
	ModelVersion   string                          `json:"modelVersion"`
	ModelPublisher string                          `json:"modelPublisher"`
	Capabilities   azureDeploymentCapabilitiesJSON `json:"capabilities"`
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
//
// The third argument is the project-scoped deployments endpoint used for live
// discovery. Provider ingress still resolves the resource root from the
// operator-selected locator and only uses the project endpoint as a fallback
// input for that normalization.
func NewRuntime(client *http.Client, credentials providersruntime.CredentialProvider, azureProjectEndpoint string) providersruntime.ProviderRuntimeBundle {
	if client == nil {
		client = http.DefaultClient
	}
	projectEndpoint := strings.TrimSpace(azureProjectEndpoint) // swobu:io-string source=boundary
	anthropicClient := cloneHTTPClientWithAuthHeaderMapping(client, "x-api-key", "api-key")
	router := azureProviderIngressResolver{
		openAI:          openaifamily.NewExecutor(client, credentials, NewPolicy()),
		anthropic:       anthropicprovider.NewExecutor(anthropicClient, credentials),
		projectEndpoint: projectEndpoint,
	}
	return providersruntime.ProviderRuntimeBundle{
		ProviderID:         profile.ProviderSpecAzure,
		ProviderExecutor:   router,
		CredentialProvider: credentials,
		Discovery: azureProviderModelCatalogClient{
			client:          client,
			credentials:     credentials,
			projectEndpoint: projectEndpoint,
		},
	}
}

func cloneHTTPClientWithAuthHeaderMapping(client *http.Client, from string, to string) *http.Client {
	if client == nil {
		client = http.DefaultClient
	}
	clone := *client
	clone.Transport = authHeaderMappingRoundTripper{
		base: client.Transport,
		from: from,
		to:   to,
	}
	return &clone
}

func (rt authHeaderMappingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if value := strings.TrimSpace(clone.Header.Get(rt.from)); value != "" { // swobu:io-string source=boundary
		clone.Header.Del(rt.from)
		if clone.Header.Get(rt.to) == "" {
			clone.Header.Set(rt.to, value)
		}
	}
	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

func (r azureProviderIngressResolver) ResolveProviderIngress(ctx context.Context, req exchange.ProviderRequest) (exchange.ProviderIngress, error) {
	baseURL, err := resolveAzureResourceRoot(req.Target.BaseURL, r.projectEndpoint)
	if err != nil {
		return nil, canonical.BadEndpoint("azure resource locator is required")
	}
	next := req
	switch req.Target.ProtocolKind {
	case protocolkind.Messages:
		next.Target.BaseURL = strings.TrimRight(baseURL, "/") + "/anthropic/v1"
		return r.anthropic.ResolveProviderIngress(ctx, next)
	default:
		next.Target.BaseURL = strings.TrimRight(baseURL, "/") + "/openai/v1"
		return r.openAI.ResolveProviderIngress(ctx, next)
	}
}

func (c azureProviderModelCatalogClient) ValidateCredentials(ctx context.Context, target exchange.RoutableTarget) error {
	_, err := c.ListDeployments(ctx, target)
	return err
}

func (c azureProviderModelCatalogClient) ListDeployments(ctx context.Context, target exchange.RoutableTarget) ([]profile.ProviderDeploymentRecord, error) {
	projectEndpoint, err := resolveAzureProjectEndpoint(c.projectEndpoint, target.BaseURL)
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

func (c azureProviderModelCatalogClient) listDeploymentsPage(ctx context.Context, target exchange.RoutableTarget, requestURL string) ([]profile.ProviderDeploymentRecord, string, error) {
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
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, "", canonical.BadEndpoint("azure deployment inventory request failed")
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

func (c azureProviderModelCatalogClient) applyCredential(ctx context.Context, req *http.Request, target exchange.RoutableTarget) error {
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
	capabilities := doc.Capabilities.facts()
	family := deploymentFamilyForDeployment(doc.ModelPublisher, capabilities)
	supportedProtocols := supportedProviderProtocolsForDeployment(doc.ModelPublisher, capabilities)
	defaultProtocol := defaultProviderProtocolForDeployment(doc.ModelPublisher, capabilities)
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

func resolveAzureProjectEndpoint(raw string, fallback string) (string, error) {
	candidate := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if candidate == "" {
		candidate = strings.TrimSpace(fallback) // swobu:io-string source=boundary
	}
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

// swobu:lint ignore string-switch because=azure deployment family string comes from external catalog JSON and must switch on raw wire value.
func defaultProviderProtocolForDeployment(modelPublisher string, capabilities azureDeploymentCapabilityRecord) string {
	family := deploymentFamilyForDeployment(modelPublisher, capabilities)
	switch family {
	case azureDeploymentFamilyAnthropic:
		return azureSupportedProviderProtocolsAnthropic[0]
	case azureDeploymentFamilyOpenAI:
		if capabilities.Completion && !capabilities.ChatCompletion {
			return "completions"
		}
		return azureSupportedProviderProtocolsOpenAI[0]
	default:
		return ""
	}
}

func resolveAzureResourceRoot(raw string, fallback string) (string, error) {
	candidate := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if candidate == "" {
		candidate = strings.TrimSpace(fallback) // swobu:io-string source=boundary
	}
	if candidate == "" {
		return "", fmt.Errorf("azure resource locator is required")
	}
	return endpointintent.NormalizeAzureResourceLocator(candidate)
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
