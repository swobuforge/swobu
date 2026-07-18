package adapters

import (
	"strings"
	"testing"
)

func TestInvalidBedrockDiagnosticsFailAtCockpitAdapterBoundary(t *testing.T) {
	for _, raw := range []string{`{`, `{"authentication":"future_extension"}`} {
		if _, err := decodeBedrockAuthenticationDiagnostics([]byte(raw)); err == nil || !strings.Contains(err.Error(), "invalid Bedrock authentication diagnostics") {
			t.Fatalf("decode %q error = %v", raw, err)
		}
	}
}
