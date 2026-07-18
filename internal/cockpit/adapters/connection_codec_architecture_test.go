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
