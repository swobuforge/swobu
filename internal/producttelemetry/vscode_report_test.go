package producttelemetry

import (
	"testing"

	"github.com/swobuforge/swobu/internal/domain/clientfamily"
)

func TestProductReportV3CarriesVSCodeClientFamily(t *testing.T) {
	if productReportSchemaVersion != 3 {
		t.Fatalf("schema = %d, want 3", productReportSchemaVersion)
	}
	if got := reportClientFamily(clientfamily.VSCode); got != reportClientFamilyVSCode {
		t.Fatalf("VS Code family = %q, want %q", got, reportClientFamilyVSCode)
	}
}
