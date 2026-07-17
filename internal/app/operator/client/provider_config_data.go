// Runtime lane: endpoint CRUD DTOs and Surface A helpers over daemon HTTP APIs.
package operatorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// EndpointData is a product-safe read DTO for endpoint queries via Surface A.
type EndpointData struct {
	Name            string
	SelectedRef     string
	ProviderConfigs []ProviderConfigData
}

// ProviderConfigData is a product-safe DTO for a single provider config.
type ProviderConfigData struct {
	Ref              string
	ProviderSpec     string
	BaseURL          string
	AuthMode         string
	AuthHeader       string
	CredentialRef    string
	RouteModelID     string
	ModelID          string
	TargetAlias      string
	TargetRank       int
	TargetWeight     int
	ProviderProtocol string
}

// UpsertEndpoint creates or replaces an endpoint via Surface A PUT /_swobu/endpoints/{name}.
func (c *Client) UpsertEndpoint(ctx context.Context, d EndpointData) error {
	name := strings.TrimSpace(d.Name) // swobu:io-string source=boundary
	if name == "" {
		return fmt.Errorf("operator client: endpoint name is required")
	}
	doc := endpointDataToEndpointDocument(d)
	raw, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("operator client: endpoint payload could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/_swobu/endpoints/"+name, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("operator client: endpoint save request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("operator client: endpoint save is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errorFromResponse(resp, "operator client: endpoint save failed")
	}
	return nil
}

// ListEndpoints returns all endpoints via Surface A GET /_swobu/endpoints.
func (c *Client) ListEndpoints(ctx context.Context) ([]EndpointData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_swobu/endpoints", nil)
	if err != nil {
		return nil, fmt.Errorf("operator client: list request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("operator client: endpoint list is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errorFromResponse(resp, "operator client: endpoint list failed")
	}
	var doc endpointListDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("operator client: endpoint list could not be decoded")
	}
	result := make([]EndpointData, 0, len(doc.Endpoints))
	for _, ed := range doc.Endpoints {
		result = append(result, endpointDocumentToEndpointData(ed))
	}
	return result, nil
}

// GetEndpoint returns a single endpoint by name via Surface A GET /_swobu/endpoints/{name}.
func (c *Client) GetEndpoint(ctx context.Context, name string) (EndpointData, error) {
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return EndpointData{}, fmt.Errorf("operator client: name is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_swobu/endpoints/"+name, nil)
	if err != nil {
		return EndpointData{}, fmt.Errorf("operator client: get request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return EndpointData{}, fmt.Errorf("operator client: endpoint get is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return EndpointData{}, errorFromResponse(resp, "operator client: endpoint get failed")
	}
	var ed endpointDocument
	if err := json.NewDecoder(resp.Body).Decode(&ed); err != nil {
		return EndpointData{}, fmt.Errorf("operator client: endpoint could not be decoded")
	}
	return endpointDocumentToEndpointData(ed), nil
}

// DeleteEndpoint deletes an endpoint via Surface A DELETE /_swobu/endpoints/{name}.
func (c *Client) DeleteEndpoint(ctx context.Context, name string) error {
	name = strings.TrimSpace(name) // swobu:io-string source=boundary
	if name == "" {
		return fmt.Errorf("operator client: endpoint name is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/_swobu/endpoints/"+name, nil)
	if err != nil {
		return fmt.Errorf("operator client: delete request could not be built")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("operator client: endpoint delete is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return errorFromResponse(resp, "operator client: endpoint delete failed")
	}
	return nil
}

func endpointDocumentToEndpointData(ed endpointDocument) EndpointData {
	pcs := make([]ProviderConfigData, 0, len(ed.ProviderConfigs))
	for _, pc := range ed.ProviderConfigs {
		tr, tw := 1, 1
		if pc.TargetRank != nil {
			tr = *pc.TargetRank
		}
		if pc.TargetWeight != nil {
			tw = *pc.TargetWeight
		}
		pcs = append(pcs, ProviderConfigData{
			Ref:              pc.Ref,
			ProviderSpec:     pc.ProviderSpec,
			BaseURL:          pc.BaseURL,
			AuthMode:         pc.AuthMode,
			AuthHeader:       pc.AuthHeader,
			CredentialRef:    pc.CredentialRef,
			RouteModelID:     pc.RouteModelID,
			ModelID:          pc.ModelID,
			TargetAlias:      pc.TargetAlias,
			TargetRank:       tr,
			TargetWeight:     tw,
			ProviderProtocol: pc.ProviderProtocol,
		})
	}
	return EndpointData{Name: ed.Name, SelectedRef: ed.SelectedProviderConfigRef, ProviderConfigs: pcs}
}

func endpointDataToEndpointDocument(d EndpointData) endpointDocument {
	pcs := make([]providerConfigDocument, 0, len(d.ProviderConfigs))
	for _, pc := range d.ProviderConfigs {
		tr := pc.TargetRank
		tw := pc.TargetWeight
		pcs = append(pcs, providerConfigDocument{
			Ref:              pc.Ref,
			ProviderSpec:     pc.ProviderSpec,
			BaseURL:          pc.BaseURL,
			AuthMode:         pc.AuthMode,
			AuthHeader:       pc.AuthHeader,
			CredentialRef:    pc.CredentialRef,
			RouteModelID:     pc.RouteModelID,
			ModelID:          pc.ModelID,
			TargetAlias:      pc.TargetAlias,
			TargetRank:       &tr,
			TargetWeight:     &tw,
			ProviderProtocol: pc.ProviderProtocol,
		})
	}
	return endpointDocument{Name: d.Name, SelectedProviderConfigRef: d.SelectedRef, ProviderConfigs: pcs}
}
