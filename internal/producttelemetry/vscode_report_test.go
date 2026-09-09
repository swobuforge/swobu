package producttelemetry

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/trafficevidence"
)

func TestProductReportV3CarriesVSCodeClientFamily(t *testing.T) {
	if productReportSchemaVersion != 3 {
		t.Fatalf("schema = %d, want 3", productReportSchemaVersion)
	}
	if got := reportClientFamily(trafficevidence.ClientFamilyVSCode); got != reportClientFamilyVSCode {
		t.Fatalf("VS Code family = %q, want %q", got, reportClientFamilyVSCode)
	}
}
