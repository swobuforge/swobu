package connections

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestPublicProjectionRedactsCredentialsAndStdioArguments(t *testing.T) {
	provider, _ := routing.ParseProvider("fixture", func(string) bool { return true })
	standard, err := routing.NewStandardConnection(provider, "https://example.com/api?token=QUERY_SECRET#FRAGMENT_SECRET", "env:CREDENTIAL_LOCATOR")
	if err != nil {
		t.Fatal(err)
	}
	model, err := domain.NewModelSpec(standard)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, _ := domain.ParseEndpoint("https://example.com/mcp")
	http, _ := domain.NewMCPHTTPSpec(endpoint, credentialref.Parse("secretfile:PRIVATE_LOCATOR"))
	stdio, _ := domain.NewMCPStdioSpec("/private/EXECUTABLE_DIRECTORY/server", []string{"ARGUMENT_SECRET"}, map[string]credentialref.Ref{"PRIVATE_ENVIRONMENT_NAME": credentialref.Parse("env:ENVIRONMENT_LOCATOR")})
	searchEndpoint, _ := domain.ParseEndpoint("https://example.com/search")
	search, _ := domain.NewSearchSpec("search", searchEndpoint, credentialref.Parse("env:SEARCH_LOCATOR"))
	zai, _ := routing.NewZAIConnection(provider, routing.ZAIAccessGeneralAPI, "env:ZAI_LOCATOR")
	region, _ := routing.ParseBedrockRegion("us-east-1")
	bedrock, _ := routing.NewBedrockConnection(provider, region, "https://bedrock.example.com", "env:BEDROCK_LOCATOR")
	auth, _ := routing.NewCustomHeaderAuth("X-Api-Key", "env:CUSTOM_LOCATOR")
	custom, _ := routing.NewCustomConnection(provider, "https://custom.example.com", auth)
	specs := []domain.Spec{model, http, stdio, search}
	for _, upstream := range []routing.Connection{zai, bedrock, custom} {
		spec, err := domain.NewModelSpec(upstream)
		if err != nil {
			t.Fatal(err)
		}
		specs = append(specs, spec)
	}
	for _, spec := range specs {
		id, _ := connectionidentity.New()
		value, err := domain.New(id, "Shared", spec, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		view, err := Project(value, nil)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"QUERY_SECRET", "FRAGMENT_SECRET", "LOCATOR", "ARGUMENT_SECRET", "EXECUTABLE_DIRECTORY", "PRIVATE_ENVIRONMENT_NAME", "credential_ref", "env_refs"} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("%T projection exposed %s", spec, forbidden)
			}
		}
		if view.ID != id.String() || view.Revision != "1" || view.Kind != spec.Kind() {
			t.Fatal("projection lost identity")
		}
		if stdio, ok := view.Spec.(MCPStdio); ok && (stdio.Executable != "server" || stdio.ArgumentCount != 1 || stdio.EnvironmentBindingCount != 1) {
			t.Fatal("stdio projection incorrect")
		}
	}
}

func TestProjectionIncludesOnlyMatchingRevisionHealth(t *testing.T) {
	id, _ := connectionidentity.New()
	endpoint, _ := domain.ParseEndpoint("https://example.com/mcp")
	spec, _ := domain.NewMCPHTTPSpec(endpoint, credentialref.Ref{})
	value, err := domain.New(id, "Shared", spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	check, err := domain.NewCheck(value, value.UpdatedAt(), domain.Reachable, domain.AuthenticationUnknown, nil, domain.CheckOK)
	if err != nil {
		t.Fatal(err)
	}
	view, err := Project(value, &check)
	if err != nil || view.LatestCheck == nil || view.LatestCheck.Capabilities == nil {
		t.Fatal("matching observation missing")
	}
	updated, err := value.EditSameIdentity(value.Revision(), "Renamed", spec, value.UpdatedAt().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	view, err = Project(updated, &check)
	if err != nil || view.LatestCheck != nil {
		t.Fatal("stale observation projected")
	}
	otherID, _ := connectionidentity.New()
	other, err := domain.New(otherID, "Other", spec, value.CreatedAt())
	if err != nil {
		t.Fatal(err)
	}
	view, err = Project(other, &check)
	if err != nil || view.LatestCheck != nil {
		t.Fatal("other identity observation projected")
	}
	if _, err := Project(domain.Connection{}, nil); err == nil {
		t.Fatal("zero Connection projected")
	}
}
