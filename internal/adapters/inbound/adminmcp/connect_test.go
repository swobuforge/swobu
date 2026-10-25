package adminmcp

import (
	"encoding/json"
	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
	"strings"
	"testing"
)

func TestClientPlanDoesNotDisclosePrivateBeforeOrAfterValues(t *testing.T) {
	const secret = "synthetic-host-only-material"
	// A credential can be misplaced in an ordinary field. Redacting only the
	// field called API key would leak the endpoint's current private value.
	plan := connectapp.Plan{Revision: "opaque-review", Changes: []connectapp.Change{{Field: "endpoint", Before: secret, BeforeExists: true, After: "https://example.invalid"}, {Field: "API key", After: secret}}}
	raw, err := json.Marshal(projectClientPlan(plan))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "https://example.invalid") {
		t.Fatal("client-private values entered a model projection")
	}
	if !strings.Contains(string(raw), "opaque-review") || !strings.Contains(string(raw), "endpoint") {
		t.Fatal("review identity or useful change metadata lost")
	}
}
