package connections

import (
	"encoding/json"
	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"testing"
	"time"

	domain "github.com/swobuforge/swobu/internal/connections"
	"github.com/swobuforge/swobu/internal/profile"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestAuthoringClosedSpecsAndProviderAdmission(t *testing.T) {
	for _, test := range []struct {
		kind domain.Kind
		spec string
	}{
		{domain.Model, `{"shape":"standard","provider_id":"openai","configuration":{"credential_ref":"env:TOKEN"}}`},
		{domain.Model, `{"shape":"zai","provider_id":"zai","configuration":{"access":"general_api","credential_ref":"env:TOKEN"}}`},
		{domain.Model, `{"shape":"bedrock","provider_id":"bedrock","configuration":{"region":"us-east-1","endpoint":"https://bedrock-runtime.us-east-1.amazonaws.com","credential_ref":"env:TOKEN"}}`},
		{domain.Model, `{"shape":"custom","provider_id":"custom","configuration":{"endpoint":"https://example.com/v1"}}`},
		{domain.MCP, `{"transport":"streamable_http","configuration":{"endpoint":"https://example.com/mcp","credential_ref":"env:TOKEN"}}`},
		{domain.MCP, `{"transport":"stdio","configuration":{"executable":"node","args":["server.js"],"env_refs":{"TOKEN":"env:TOKEN"}}}`},
		{domain.Search, `{"provider_id":"brave","endpoint":"https://example.com/search"}`},
	} {
		t.Run(string(test.kind)+test.spec, func(t *testing.T) {
			draft := Draft{DisplayName: "Shared", Kind: test.kind, Spec: json.RawMessage(test.spec)}
			spec, err := draft.ValidatedSpec(func(string) bool { return true })
			if err != nil || spec.Kind() != test.kind {
				t.Fatalf("invalid draft: %v", err)
			}
			if test.kind == domain.Model {
				if _, err := draft.ValidatedSpec(func(string) bool { return false }); err == nil {
					t.Fatal("unsupported provider admitted")
				}
			}
		})
	}
	for _, spec := range []string{
		`{"transport":"stdio","configuration":{"executable":"node","secret":"private"}}`,
		`{"transport":"stdio","configuration":{"executable":"node","executable":"sh"}}`,
		`{"transport":"streamable_http","configuration":{"endpoint":"https://user:token@example.com/mcp"}}`,
		`{"transport":"streamable_http","configuration":{"endpoint":"https://example.com","credential_ref":"raw-secret"}}`,
		`{"transport":"stdio","configuration":{"executable":"node","env_refs":{"TOKEN":null}}}`,
	} {
		if _, err := (Draft{DisplayName: "Shared", Kind: domain.MCP, Spec: json.RawMessage(spec)}).ValidatedSpec(nil); err == nil {
			t.Fatalf("accepted unsafe authoring %s", spec)
		}
	}
	for _, raw := range []string{`{"display_name":"one","display_name":"two","kind":"mcp","spec":{}}`, `{"display_name":"one","unknown":true}`, `{"display_name":null}`} {
		var draft Draft
		if json.Unmarshal([]byte(raw), &draft) == nil {
			t.Fatal("accepted ambiguous draft")
		}
	}
}

func TestAuthoringPreservesProviderOwnedShapeAndLocatorRules(t *testing.T) {
	for _, raw := range []string{
		`{"shape":"standard","provider_id":"bedrock","configuration":{"credential_ref":"env:TOKEN"}}`,
		`{"shape":"standard","provider_id":"openai","configuration":{"endpoint":"https://wrong.example/v1","credential_ref":"env:TOKEN"}}`,
		`{"shape":"standard","provider_id":"azure","configuration":{"credential_ref":"env:TOKEN"}}`,
		`{"shape":"standard","provider_id":"openai","configuration":{}}`,
		`{"shape":"zai","provider_id":"openai","configuration":{"access":"general_api","credential_ref":"env:TOKEN"}}`,
		`{"shape":"bedrock","provider_id":"openai","configuration":{"region":"us-east-1","endpoint":"https://example.com"}}`,
		`{"shape":"custom","provider_id":"openai","configuration":{"endpoint":"https://example.com"}}`,
		`{"shape":"standard","provider_id":"openai","configuration":{"endpoint":"https://example.com?token=private","credential_ref":"env:TOKEN"}}`,
		`{"shape":"bedrock","provider_id":"bedrock","configuration":{"region":"invalid","endpoint":"https://example.com"}}`,
		`{"shape":"bedrock","provider_id":"bedrock","configuration":{"region":"us-east-1","endpoint":"https://bedrock-mantle.eu-west-1.api.aws/v1"}}`,
	} {
		if _, err := (Draft{DisplayName: "Shared", Kind: domain.Model, Spec: json.RawMessage(raw)}).ValidatedSpec(profile.SupportsSpec); err == nil {
			t.Fatal("provider rule bypassed", raw)
		}
	}
}

// The catalog is the eligible universe, including providers added later. Every
// provider must admit its own shape and reject each other closed shape through
// the same authoring boundary used by create and edit.
func TestAuthoringProviderCatalogShapeCoverage(t *testing.T) {
	for _, entry := range profile.All() {
		t.Run(string(entry.ProviderID), func(t *testing.T) {
			endpoint := ""
			switch entry.Locator.Kind {
			case profile.LocatorAzureProject:
				endpoint = "https://example.services.ai.azure.com/api/projects/demo"
			case profile.LocatorBaseURL:
				if entry.Locator.Default == "" {
					endpoint = "https://example.com/v1"
				}
			}
			credential := "env:SYNTHETIC_TOKEN"
			if entry.Credential.Requirement == profile.CredentialUnsupported {
				credential = ""
			}
			for _, variant := range []struct {
				name          string
				shape         routing.ConnectionShape
				configuration map[string]string
			}{
				{"standard", routing.ConnectionShapeStandard, map[string]string{"endpoint": endpoint, "credential_ref": credential}},
				{"zai", routing.ConnectionShapeZAI, map[string]string{"access": "general_api", "credential_ref": "env:SYNTHETIC_TOKEN"}},
				{"bedrock", routing.ConnectionShapeBedrock, map[string]string{"region": "us-east-1", "endpoint": "https://bedrock-runtime.us-east-1.amazonaws.com"}},
				{"custom", routing.ConnectionShapeCustom, map[string]string{"endpoint": "https://example.com/v1"}},
			} {
				t.Run(variant.name, func(t *testing.T) {
					raw, err := json.Marshal(map[string]any{"shape": variant.name, "provider_id": entry.ProviderID, "configuration": variant.configuration})
					if err != nil {
						t.Fatal(err)
					}
					_, err = (Draft{DisplayName: "Shared", Kind: domain.Model, Spec: raw}).ValidatedSpec(profile.SupportsSpec)
					if want := variant.shape == entry.ConnectionShape; (err == nil) != want {
						t.Fatalf("admitted=%v want=%v: %v", err == nil, want, err)
					}
				})
			}
		})
	}
}

func TestAuthoringBedrockEndpointUsesProviderNormalization(t *testing.T) {
	raw := `{"shape":"bedrock","provider_id":"bedrock","configuration":{"region":"us-east-1","endpoint":"https://bedrock-mantle.us-east-1.api.aws/anthropic/v1/messages"}}`
	spec, err := (Draft{DisplayName: "Shared", Kind: domain.Model, Spec: json.RawMessage(raw)}).ValidatedSpec(profile.SupportsSpec)
	if err != nil {
		t.Fatal(err)
	}
	upstream := spec.(domain.ModelSpec).Upstream().(routing.BedrockConnection)
	if upstream.Endpoint() != "https://bedrock-mantle.us-east-1.api.aws/anthropic/v1" {
		t.Fatalf("endpoint=%s", upstream.Endpoint())
	}
}

func TestMCPAuthoringPreservesUnverifiedDeclarations(t *testing.T) {
	for _, configuration := range []string{
		`{"transport":"streamable_http","configuration":{"endpoint":"https://example.com/mcp"}}`,
		`{"transport":"stdio","configuration":{"executable":"server","args":[],"env_refs":{}}}`,
	} {
		var payload map[string]any
		if err := json.Unmarshal([]byte(configuration), &payload); err != nil {
			t.Fatal(err)
		}
		payload["execution_identity"] = "Declared account"
		payload["permission_summary"] = "Read and write documents"
		raw, _ := json.Marshal(payload)
		spec, err := (Draft{DisplayName: "Docs", Kind: domain.MCP, Spec: raw}).ValidatedSpec(nil)
		if err != nil {
			t.Fatal(err)
		}
		declarations, ok := spec.(interface {
			ExecutionIdentity() string
			PermissionSummary() string
		})
		if !ok || declarations.ExecutionIdentity() != "Declared account" || declarations.PermissionSummary() != "Read and write documents" {
			t.Fatal("declaration lost during authoring")
		}
		id, _ := connectionidentity.Parse("cn_declared")
		value, err := domain.New(id, "Docs", spec, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		view, err := Project(value, nil)
		if err != nil {
			t.Fatal(err)
		}
		projected, err := json.Marshal(view.Spec)
		if err != nil {
			t.Fatal(err)
		}
		var observed map[string]any
		if json.Unmarshal(projected, &observed) != nil || observed["execution_identity"] != "Declared account" || observed["permission_summary"] != "Read and write documents" || observed["identity_verified"] != false {
			t.Fatal("projection lost unverified declaration truth")
		}
		payload["execution_identity"] = " invalid"
		raw, _ = json.Marshal(payload)
		if _, err := (Draft{DisplayName: "Docs", Kind: domain.MCP, Spec: raw}).ValidatedSpec(nil); err == nil {
			t.Fatal("invalid declaration admitted")
		}
	}
}
