package producttelemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/swobuforge/swobu/internal/delivery"
	"github.com/swobuforge/swobu/internal/domain/canonical"
	"github.com/swobuforge/swobu/internal/profile"
)

// currentGoldenReport is the deterministic report produced by the real Go encoder
// (json.Marshal of ProductReport) for the current schema version. It is the single
// source of truth for the committed
// contracts/product-report-v<schema>.example.json; regenerate that file with
// SWOBU_REGENERATE_GOLDEN=1 (see TestCurrentGoldenExample_Regenerate). It is NOT a
// separately designed fixture.
func currentGoldenReport() productReport {
	return productReport{
		Schema:                productReportSchemaVersion,
		InstallID:             "0123456789abcdef0123456789abcdef",
		Runtime:               reportRuntime{Version: "0.1.0", OS: "linux", Arch: "amd64"},
		InstallationAgeBucket: "1_7d",
		Traffic: []reportTrafficRow{
			{
				UserAgentProduct:   "codex-cli/0.42.0",
				RequestPath:        canonical.NormalizedPathResponses,
				Provider:           profile.ProviderSpecOpenAI,
				StatusCode:         200,
				DeliveryKind:       delivery.Succeeded,
				CanonicalErrorCode: "",
				AttemptCount:       1,
				FallbackRecovered:  false,
				Count:              27,
				DurationMS:         [6]int{0, 2, 14, 9, 2, 0},
				TTFBMS:             [6]int{0, 5, 18, 3, 1, 0},
			},
			{
				UserAgentProduct:   "claude-code/1.0.0",
				RequestPath:        canonical.NormalizedPathMessages,
				Provider:           profile.ProviderSpecAnthropic,
				StatusCode:         503,
				DeliveryKind:       delivery.ExchangeFailed,
				CanonicalErrorCode: canonical.ErrorCodeBadEndpoint,
				AttemptCount:       2,
				FallbackRecovered:  true,
				Count:              3,
				DurationMS:         [6]int{0, 0, 1, 1, 1, 0},
				TTFBMS:             [6]int{0, 0, 1, 1, 0, 1},
			},
		},
		OverflowCount: 0,
	}
}

func contractsDir() string {
	return filepath.Join("..", "..", "..", "..", "swobucom", "apps", "ingest-api", "contracts")
}

// productReportExamplePath is the committed golden fixture path for one schema
// version. The Go client proves only the CURRENT emitter version's example exists
// and matches the encoder; the Worker owns the registry-completeness invariant
// (every published version registered == schema files == example files).
func productReportExamplePath(version int) string {
	return filepath.Join(contractsDir(), "product-report-v"+strconv.Itoa(version)+".example.json")
}

// TestCurrentGoldenExample_MatchesEncoder proves the committed example for the
// current schema version is semantically equal to the real Go encoder's output
// for currentGoldenReport (so the fixture cannot drift from the struct).
func TestCurrentGoldenExample_MatchesEncoder(t *testing.T) {
	body, err := os.ReadFile(productReportExamplePath(productReportSchemaVersion))
	if err != nil {
		t.Fatalf("read committed example: %v (regenerate with SWOBU_REGENERATE_GOLDEN=1)", err)
	}
	var fromFile productReport
	if err := json.Unmarshal(body, &fromFile); err != nil {
		t.Fatalf("decode committed example: %v", err)
	}
	if expected := currentGoldenReport(); !reflect.DeepEqual(fromFile, expected) {
		enc, _ := json.MarshalIndent(expected, "", "  ")
		t.Fatalf("committed example is not the encoder output for currentGoldenReport:\nfile:   %s\nexpect: %s", body, enc)
	}
}

// TestCurrentGoldenExample_Regenerate rewrites the committed example from the
// encoder output. Skipped unless SWOBU_REGENERATE_GOLDEN=1, so normal runs verify
// (not write) the fixture.
func TestCurrentGoldenExample_Regenerate(t *testing.T) {
	if os.Getenv("SWOBU_REGENERATE_GOLDEN") != "1" {
		t.Skip("set SWOBU_REGENERATE_GOLDEN=1 to regenerate the committed example")
	}
	body, err := json.MarshalIndent(currentGoldenReport(), "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	if err := os.WriteFile(productReportExamplePath(productReportSchemaVersion), append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write example: %v", err)
	}
	t.Logf("regenerated %s", productReportExamplePath(productReportSchemaVersion))
}
