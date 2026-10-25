package connections

import (
	"github.com/swobuforge/swobu/internal/domain/credentialref"
	"github.com/swobuforge/swobu/internal/routing"
	"testing"
)

func TestEndpointExcludesSecretBearingURLParts(t *testing.T) {
	for _, raw := range []string{"", "https://user:password@example.com", "https://example.com?token=secret", "https://example.com#secret", "file:///tmp/socket", "https://example.com?", " https://example.com", "https://example.com\\path"} {
		if _, err := ParseEndpoint(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"https://example.com/api", "http://127.0.0.1:8080/mcp"} {
		endpoint, err := ParseEndpoint(raw)
		if err != nil || endpoint.String() != raw {
			t.Fatalf("endpoint=%v err=%v", endpoint, err)
		}
	}
}
func TestSpecsRequireValidatedConfiguration(t *testing.T) {
	if _, err := NewModelSpec(nil); err == nil {
		t.Fatal("accepted nil model")
	}
	if _, err := NewModelSpec(routing.StandardConnection{}); err == nil {
		t.Fatal("accepted zero model")
	}
	if _, err := NewMCPHTTPSpec(Endpoint{}, credentialref.Ref{}); err == nil {
		t.Fatal("accepted missing endpoint")
	}
	endpoint, _ := ParseEndpoint("https://example.com/mcp")
	for _, raw := range []string{"raw-secret", "env:", "secret:", "file:", "secret:value\nother"} {
		if _, err := NewMCPHTTPSpec(endpoint, credentialref.Parse(raw)); err == nil {
			t.Errorf("accepted credential %q", raw)
		}
	}
	mcp, err := NewMCPHTTPSpec(endpoint, credentialref.Parse("env:MCP_TOKEN"))
	if err != nil || mcp.Kind() != MCP {
		t.Fatal(err)
	}
	search, err := NewSearchSpec("search", endpoint, credentialref.Ref{})
	if err != nil || search.Kind() != Search {
		t.Fatal(err)
	}
}
func TestStdioKeepsArgumentsAndEnvironmentImmutable(t *testing.T) {
	args := []string{"--serve"}
	env := map[string]credentialref.Ref{"TOKEN": credentialref.Parse("env:SERVER_TOKEN")}
	spec, err := NewMCPStdioSpec("server", args, env)
	if err != nil {
		t.Fatal(err)
	}
	args[0] = "changed"
	env["TOKEN"] = credentialref.Parse("env:OTHER")
	returned := spec.Args()
	returned[0] = "again"
	values := spec.Environment()
	delete(values, "TOKEN")
	if spec.Args()[0] != "--serve" || spec.Environment()["TOKEN"].String() != "env:SERVER_TOKEN" {
		t.Fatal("mutable stdio configuration escaped")
	}
	for _, command := range []string{"", "server --serve", "server\nother"} {
		if _, err := NewMCPStdioSpec(command, nil, nil); err == nil {
			t.Errorf("accepted %q", command)
		}
	}
	if _, err := NewMCPStdioSpec("server", nil, map[string]credentialref.Ref{"TOKEN": credentialref.Parse("raw-secret")}); err == nil {
		t.Fatal("accepted raw environment secret")
	}
}
