package connections

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCredentialReplacementPreservesAllOtherSpecFields(t *testing.T) {
	old := credentialref.Parse("env:ORIGINAL")
	next := credentialref.Parse("env:REPLACEMENT")
	provider, _ := routing.ParseProvider("fixture", func(string) bool { return true })
	standard, _ := routing.NewStandardConnection(provider, "https://example.com/api", old.String())
	zai, _ := routing.NewZAIConnection(provider, routing.ZAIAccessCodingPlan, old.String())
	region, _ := routing.ParseBedrockRegion("us-east-1")
	bedrock, _ := routing.NewBedrockConnection(provider, region, "https://bedrock.example.com", old.String())
	auth, _ := routing.NewCustomHeaderAuth("X-API-Key", old.String())
	custom, _ := routing.NewCustomConnection(provider, "https://custom.example.com", auth)
	var specs []Spec
	for _, upstream := range []routing.Connection{standard, zai, bedrock, custom} {
		spec, err := NewModelSpec(upstream)
		if err != nil {
			t.Fatal(err)
		}
		specs = append(specs, spec)
	}
	endpoint, _ := ParseEndpoint("https://example.com/mcp")
	http, _ := NewMCPHTTPSpec(endpoint, old)
	stdio, _ := NewMCPStdioSpec("server", []string{"--serve"}, map[string]credentialref.Ref{"TOKEN": old, "OTHER": old})
	search, _ := NewSearchSpec("search", endpoint, old)
	specs = append(specs, http, stdio, search)
	base := fixtureConnection(t)
	for _, spec := range specs {
		t.Run(reflect.TypeOf(spec).String()+string(spec.Kind()), func(t *testing.T) {
			original, err := New(base.ID(), "Same identity", spec, base.CreatedAt())
			if err != nil {
				t.Fatal(err)
			}
			environment := ""
			if _, ok := spec.(MCPStdioSpec); ok {
				environment = "TOKEN"
			}
			changed, err := original.ReplaceCredential(original.Revision(), environment, old, next, original.UpdatedAt().Add(time.Second))
			if err != nil {
				t.Fatal(err)
			}
			if changed.ID() != original.ID() || changed.Revision().String() != "2" || changed.DisplayName() != original.DisplayName() || changed.CreatedAt() != original.CreatedAt() {
				t.Fatal("replacement changed identity")
			}
			// Reversibility proves no unrelated field was dropped or changed.
			restored, err := changed.ReplaceCredential(changed.Revision(), environment, next, old, changed.UpdatedAt().Add(time.Second))
			if err != nil || !reflect.DeepEqual(restored.Spec(), original.Spec()) {
				t.Fatal("replacement changed noncredential semantics")
			}
			if stdio, ok := changed.Spec().(MCPStdioSpec); ok && stdio.Environment()["OTHER"] != old {
				t.Fatal("replacement changed unselected environment binding")
			}
			if _, err := changed.ReplaceCredential(original.Revision(), environment, old, next, changed.UpdatedAt()); !errors.Is(err, ErrRevisionConflict) {
				t.Fatalf("stale replacement: %v", err)
			}
			if _, err := changed.ReplaceCredential(changed.Revision(), environment, old, next, changed.UpdatedAt()); !errors.Is(err, ErrCredentialBinding) {
				t.Fatalf("stale binding: %v", err)
			}
			if !reflect.DeepEqual(original.Spec(), spec) {
				t.Fatal("original mutated")
			}
		})
	}
}

func TestCredentialReplacementRejectsInvalidRefsWithoutEcho(t *testing.T) {
	c := fixtureConnection(t)
	for _, raw := range []string{"", "literal-secret-do-not-echo", "env:", "secret:value\nextra"} {
		_, err := c.ReplaceCredential(c.Revision(), "", credentialref.Parse("env:MCP_TOKEN"), credentialref.Parse(raw), c.UpdatedAt())
		if err == nil || raw != "" && strings.Contains(err.Error(), raw) {
			t.Fatal("invalid reference accepted or echoed")
		}
	}
	if _, err := c.ReplaceCredential(c.Revision(), "TOKEN", credentialref.Parse("env:MCP_TOKEN"), credentialref.Parse("env:NEXT"), c.UpdatedAt()); !errors.Is(err, ErrCredentialBinding) {
		t.Fatal("non-stdio environment accepted")
	}
}
