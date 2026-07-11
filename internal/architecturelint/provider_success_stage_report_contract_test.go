package architecturelint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderExecutorsReturnTransportCarriersOnly(t *testing.T) {
	t.Parallel()

	root := fromHere(t, "..")
	files := []string{
		filepath.Join(root, "adapters", "outbound", "providers", "openaifamily", "executor.go"),
		filepath.Join(root, "adapters", "outbound", "providers", "anthropic", "executor.go"),
		filepath.Join(root, "adapters", "outbound", "providers", "chatgpt", "executor.go"),
		filepath.Join(root, "adapters", "outbound", "providers", "bedrock", "executor.go"),
	}

	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(raw)
		if !strings.Contains(text, "ProviderTransportResponse") {
			t.Fatalf("provider executor %s must return provider transport carriers", path)
		}
		forbidden := []string{
			strings.Join([]string{"Build", "Provider", "Success", "Metadata("}, ""),
			strings.Join([]string{"New", "Provider", "Response", "With", "Stages("}, ""),
			strings.Join([]string{"Attach", "Provider", "Success", "From"}, ""),
			strings.Join([]string{"Decode", "Provider", "HTTP", "Success("}, ""),
		}
		hasForbidden := false
		for _, token := range forbidden {
			if strings.Contains(text, token) {
				hasForbidden = true
				break
			}
		}
		if hasForbidden {
			t.Fatalf("provider executor %s must not attach provider success metadata", path)
		}
	}
}
