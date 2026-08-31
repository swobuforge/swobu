package producttelemetry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/domain/protocolkind"
)

func TestProductReport_MarshalCarriesSchemaAndInstallID(t *testing.T) {
	installID, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	r := productReport{
		Schema:          productReportSchemaVersion,
		ReportID:        installID,
		ReportCreatedAt: "2026-08-31T12:00:00Z",
		InstallID:       installID,
		Traffic:         []reportTrafficRow{{TargetProtocol: protocolkind.Responses, Count: 3}},
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	for _, want := range []string{`"schema":2`, `"report_id":`, `"report_created_at":`, `"install_id":`, `"traffic":`} {
		if !strings.Contains(s, want) {
			t.Fatalf("report JSON missing %q: %s", want, s)
		}
	}
	if strings.Contains(s, "user_agent_product") || strings.Contains(s, "request_path") {
		t.Fatalf("report contains retired V1 dimensions: %s", s)
	}
}
