package architecturelint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeliveryContracts_DoNotUseStreamingBoolSignatures(t *testing.T) {
	t.Parallel()

	roots := []string{
		filepath.Join("..", "exchange"),
		filepath.Join("..", "adapters", "inbound", "httpapi"),
		filepath.Join("..", "ports"),
	}
	for _, rel := range roots {
		root := fromHere(t, rel)
		if _, statErr := os.Stat(root); statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			t.Fatalf("stat %s: %v", root, statErr)
		}
		filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				t.Fatalf("walk %s: %v", root, err)
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatalf("read %s: %v", path, readErr)
			}
			text := string(raw)
			if strings.Contains(text, "streaming bool") {
				t.Fatalf("forbidden bool delivery signature in %s", path)
			}
			responseModeToken := "Response" + "Mode"
			if strings.Contains(text, responseModeToken+"FromStreaming") {
				t.Fatalf("forbidden bool-conversion helper reference in %s", path)
			}
			if strings.Contains(text, responseModeToken) {
				t.Fatalf("forbidden response-mode transition residue in %s", path)
			}
			if strings.Contains(text, "NewExecutionContract(streaming") {
				t.Fatalf("forbidden bool delivery contract call in %s", path)
			}
			return nil
		})
	}
}
