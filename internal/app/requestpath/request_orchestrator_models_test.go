package requestpath

import "testing"

func TestPrimaryModelOption_IncludesPrimarySelectorForSelectedTarget(t *testing.T) {
	endpoint := testEndpointForCatalog(t, []catalogProvider{
		{ref: "backend-a", spec: "custom", model: "model-default", alias: "fast"},
		{ref: "backend-b", spec: "custom", model: "model-other"},
	}, "backend-a")
	catalog := buildEndpointModelCatalog(endpoint)

	got, ok := primaryModelOption(catalog)
	if !ok {
		t.Fatal("primaryModelOption returned ok=false, want true")
	}
	if got.ID != "primary" {
		t.Fatalf("primary id = %q, want %q", got.ID, "primary")
	}
	if got.BackendRef != "backend-a" {
		t.Fatalf("primary backend ref = %q, want %q", got.BackendRef, "backend-a")
	}
	if got.ModelID != "model-default" {
		t.Fatalf("primary model id = %q, want %q", got.ModelID, "model-default")
	}
}

func TestPrimaryModelOption_SkipsWhenCatalogAlreadyContainsPrimaryID(t *testing.T) {
	catalog := endpointModelCatalog{
		Entries: []endpointModelEntry{
			{ID: "primary", ModelID: "m", ProviderSpec: "custom", ProviderRef: "backend-a"},
		},
		DefaultRef: "backend-a",
	}

	_, ok := primaryModelOption(catalog)
	if ok {
		t.Fatal("primaryModelOption returned ok=true, want false")
	}
}
