package connections

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/domain/connectionidentity"
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCodecPreservesEverySealedSpec(t *testing.T) {
	provider := routing.Provider("fixture")
	standard, _ := routing.NewStandardConnection(provider, "https://example.com", "env:TOKEN")
	zai, _ := routing.NewZAIConnection(provider, routing.ZAIAccessGeneralAPI, "env:ZAI_TOKEN")
	region, _ := routing.ParseBedrockRegion("us-east-1")
	bedrock, _ := routing.NewBedrockConnection(provider, region, "https://bedrock.example.com", "env:AWS_TOKEN")
	auth, _ := routing.NewCustomHeaderAuth("X-Api-Key", "env:CUSTOM_TOKEN")
	custom, _ := routing.NewCustomConnection(provider, "https://custom.example.com", auth)
	specs := []Spec{}
	for _, upstream := range []routing.Connection{standard, zai, bedrock, custom} {
		spec, err := NewModelSpec(upstream)
		if err != nil {
			t.Fatal(err)
		}
		specs = append(specs, spec)
	}
	endpoint, _ := ParseEndpoint("https://example.com/mcp")
	httpSpec, _ := NewMCPHTTPSpec(endpoint, credentialref.Parse("env:MCP_TOKEN"))
	specs = append(specs, httpSpec)
	stdio, _ := NewMCPStdioSpec("server", []string{"--serve"}, map[string]credentialref.Ref{"TOKEN": credentialref.Parse("env:STDIO_TOKEN")})
	specs = append(specs, stdio)
	search, _ := NewSearchSpec("search", endpoint, credentialref.Parse("env:SEARCH_TOKEN"))
	specs = append(specs, search)
	for _, spec := range specs {
		t.Run(string(spec.Kind())+reflect.TypeOf(spec).String(), func(t *testing.T) {
			id, _ := connectionidentity.New()
			original, err := New(id, "Example", spec, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := encodeRecord(original)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(encoded)
			if err != nil {
				t.Fatal(err)
			}
			var stored record
			if err := strictDecode(raw, &stored); err != nil {
				t.Fatal(err)
			}
			decoded, err := decodeRecord(stored, func(id string) bool { return id == "fixture" })
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := encodeRecord(decoded)
			if err != nil {
				t.Fatal(err)
			}
			again, _ := json.Marshal(repeated)
			if string(raw) != string(again) || !reflect.DeepEqual(original.Spec(), decoded.Spec()) {
				t.Fatalf("roundtrip changed spec: %s vs %s", raw, again)
			}
		})
	}
}
func TestCodecRejectsUnknownFieldsKindsAndProviderAdmission(t *testing.T) {
	c := fixtureConnection(t)
	encoded, err := encodeRecord(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"unknown":true}`, `{} {}`, `null {}`} {
		var value record
		if err := strictDecode([]byte(raw), &value); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	encoded.Kind = "unknown"
	if _, err := decodeRecord(encoded, nil); err == nil {
		t.Fatal("accepted unknown kind")
	}
	upstream, _ := routing.NewStandardConnection(routing.Provider("fixture"), "", "env:TOKEN")
	spec, _ := NewModelSpec(upstream)
	model, _ := New(c.ID(), "Example", spec, c.CreatedAt())
	encoded, _ = encodeRecord(model)
	if _, err := decodeRecord(encoded, func(string) bool { return false }); err == nil {
		t.Fatal("bypassed provider admission")
	}
	encoded.Spec = json.RawMessage(`{"shape":"standard","provider":"fixture","configuration":{"endpoint":"https://example.com","secret":"do-not-echo"}}`)
	if _, err := decodeRecord(encoded, func(string) bool { return true }); err == nil || strings.Contains(err.Error(), "do-not-echo") {
		t.Fatalf("unknown field not safely rejected: %v", err)
	}
}

// Import/restart must preserve stored transport identity even when new
// authoring disallows that endpoint. Migration must not silently normalize it.
func TestCodecPreservesLegacyModelEndpoint(t *testing.T) {
	provider, err := routing.ParseProvider("openai", func(id string) bool { return id == "openai" })
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "https://legacy.example/v1?api-version=legacy"
	upstream, err := routing.NewStandardConnection(provider, endpoint, "env:LEGACY_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := NewModelSpec(upstream)
	if err != nil {
		t.Fatal(err)
	}
	base := fixtureConnection(t)
	value, err := New(base.ID(), "Imported", spec, base.CreatedAt())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := encodeRecord(value)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := decodeRecord(stored, func(id string) bool { return id == "openai" })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(value.Spec(), restored.Spec()) {
		t.Fatal("stored endpoint or credential identity changed")
	}
}

func TestSearchDecodeRejectsURLQueryOrFragment(t *testing.T) {
	endpoint, err := ParseEndpoint("https://search.example.com/api")
	if err != nil {
		t.Fatal(err)
	}
	spec, err := NewSearchSpec("search", endpoint, credentialref.Parse("env:SYNTHETIC"))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := connectionidentity.Parse("cn_search")
	value, err := New(id, "Search", spec, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := encodeRecord(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"https://example.com/api?token=private", "https://example.com/api#fragment", "https://user:secret@example.com/api", "file:///tmp/search", "https://example.com/\r\nheader", "https://example.com/\\path"} {
		raw, err := json.Marshal(searchRecord{Provider: "search", Endpoint: bad, Credential: "env:SYNTHETIC"})
		if err != nil {
			t.Fatal(err)
		}
		candidate := stored
		candidate.Spec = raw
		if _, err := decodeRecord(candidate, nil); err == nil {
			t.Fatalf("accepted invalid Search endpoint %q", bad)
		}
	}
}
