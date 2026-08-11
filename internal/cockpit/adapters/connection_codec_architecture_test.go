package adapters

import (
	"os"
	"strings"
	"testing"
)

func TestTargetSaveAndProbeShareOperatorConnectionCodec(t *testing.T) {
	for _, path := range []string{"route_projection.go", "live_operator.go"} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(content), "workspaceapi.ConnectionFromRouting") {
			t.Fatalf("%s bypasses shared operator connection codec", path)
		}
	}
	for _, forbidden := range []string{"probeParamsFromConnection", "provider_spec=", "credential_ref="} {
		content, err := os.ReadFile("live_operator.go")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), forbidden) {
			t.Fatalf("probe adapter retains flattened connection transport %q", forbidden)
		}
	}
}

func TestTargetReadProjectionUsesOperatorConnectionDecoder(t *testing.T) {
	content, err := os.ReadFile("route_projection.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	if !strings.Contains(source, "target.Connection.RoutingConnection()") {
		t.Fatal("target read projection bypasses the shared operator connection decoder")
	}
	for _, arm := range []string{"Connection.OpenAI", "Connection.Anthropic", "Connection.DeepSeek", "Connection.OpenRouter", "Connection.ZAI", "Connection.ChatGPT", "Connection.Ollama", "Connection.LMStudio", "Connection.VLLM", "Connection.Azure", "Connection.Bedrock", "Connection.Custom"} {
		if strings.Contains(source, arm) {
			t.Fatalf("target read projection retains duplicate transport-arm decoding through %q", arm)
		}
	}
}

func TestCockpitDelegatesCredentialPersistenceToDaemon(t *testing.T) {
	content, err := os.ReadFile("live_operator.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, forbidden := range []string{
		"adapters/outbound/credentials",
		"ResolveAuthCredentialWritePolicy",
		"StoreMaterializedCredential",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("Cockpit adapter owns daemon credential persistence through %q", forbidden)
		}
	}
	if !strings.Contains(source, "a.client.StorePastedCredential") {
		t.Fatal("Cockpit adapter does not delegate pasted credentials to the daemon operator client")
	}
}
