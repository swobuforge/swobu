package producttelemetry

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProductReport_MarshalCarriesSchemaAndInstallID(t *testing.T) {
	installID, err := newToken()
	if err != nil {
		t.Fatalf("newToken: %v", err)
	}
	r := productReport{
		Schema:    productReportSchemaVersion,
		InstallID: installID,
		Traffic:   []reportTrafficRow{{RequestPath: "/responses", Count: 3}},
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	for _, want := range []string{`"schema":1`, `"install_id":`, `"traffic":`} {
		if !strings.Contains(s, want) {
			t.Fatalf("report JSON missing %q: %s", want, s)
		}
	}
	if strings.Contains(s, "report_id") {
		t.Fatalf("report must not carry a report_id: %s", s)
	}
}
