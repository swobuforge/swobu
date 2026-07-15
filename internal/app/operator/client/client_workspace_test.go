package operatorclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func intPtr(v int) *int { return &v }

func TestUpsertEndpoint_CreateWithEmptyProviderConfigs(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/_swobu/endpoints/test-ws" {
			t.Fatalf("request method/path = %s %s, want PUT /_swobu/endpoints/test-ws", r.Method, r.URL.Path)
		}
		var doc endpointDocument
		if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if doc.Name != "test-ws" {
			t.Fatalf("name = %q, want test-ws", doc.Name)
		}
		if len(doc.ProviderConfigs) != 1 || doc.ProviderConfigs[0].ProviderSpec != "openai_compatible" {
			t.Fatalf("unexpected provider configs: %+v", doc.ProviderConfigs)
		}
		if *doc.ProviderConfigs[0].TargetRank != 2 || *doc.ProviderConfigs[0].TargetWeight != 4 {
			t.Fatalf("rank/weight = %d/%d, want 2/4", *doc.ProviderConfigs[0].TargetRank, *doc.ProviderConfigs[0].TargetWeight)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	err := c.UpsertEndpoint(context.Background(), EndpointData{Name: "test-ws", ProviderConfigs: []ProviderConfigData{{ProviderSpec: "openai_compatible", ModelID: "gpt-4.1", TargetRank: 2, TargetWeight: 4}}})
	if err != nil {
		t.Fatalf("UpsertEndpoint returned error: %v", err)
	}
}

func TestListEndpoints_ReadsEndpointList(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/_swobu/endpoints" {
			t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(endpointListDocument{Endpoints: []endpointDocument{
			{Name: "alpha", ProviderConfigs: []providerConfigDocument{
				{Ref: "cfg-1", ProviderSpec: "openai", ModelID: "gpt-4", TargetRank: intPtr(2), TargetWeight: intPtr(4)},
			}},
			{Name: "beta"},
		}})
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	eps, err := c.ListEndpoints(context.Background())
	if err != nil {
		t.Fatalf("ListEndpoints returned error: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("endpoints = %d, want 2", len(eps))
	}
	if eps[0].Name != "alpha" || eps[1].Name != "beta" {
		t.Fatalf("unexpected names: %q, %q", eps[0].Name, eps[1].Name)
	}
	if len(eps[0].ProviderConfigs) != 1 {
		t.Fatalf("alpha providers = %d, want 1", len(eps[0].ProviderConfigs))
	}
	if got := eps[0].ProviderConfigs[0].TargetRank; got != 2 {
		t.Fatalf("target rank = %d, want 2", got)
	}
	if got := eps[0].ProviderConfigs[0].TargetWeight; got != 4 {
		t.Fatalf("target weight = %d, want 4", got)
	}
	if len(eps[1].ProviderConfigs) != 0 {
		t.Fatalf("beta providers = %d, want 0", len(eps[1].ProviderConfigs))
	}
}

func TestGetEndpoint_ReadsSingleEndpoint(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/_swobu/endpoints/dev" {
			t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(endpointDocument{
			Name:                      "dev",
			SelectedProviderConfigRef: "cfg-1",
			ProviderConfigs: []providerConfigDocument{
				{Ref: "cfg-1", ProviderSpec: "openai", ModelID: "gpt-4"},
			},
		})
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	ep, err := c.GetEndpoint(context.Background(), "dev")
	if err != nil {
		t.Fatalf("GetEndpoint returned error: %v", err)
	}
	if ep.Name != "dev" {
		t.Fatalf("name = %q, want dev", ep.Name)
	}
	if ep.SelectedRef != "cfg-1" {
		t.Fatalf("selected ref = %q, want cfg-1", ep.SelectedRef)
	}
	if len(ep.ProviderConfigs) != 1 {
		t.Fatalf("providers = %d, want 1", len(ep.ProviderConfigs))
	}
}

func TestDeleteEndpoint_SendsDelete(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/_swobu/endpoints/dev" {
			t.Fatalf("request method/path = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c := New(server.Client(), server.URL)
	if err := c.DeleteEndpoint(context.Background(), "dev"); err != nil {
		t.Fatalf("DeleteEndpoint returned error: %v", err)
	}
}
