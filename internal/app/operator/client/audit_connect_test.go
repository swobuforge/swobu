package operatorclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/app/operator/managementaudit"
)

func TestAuditEventsRejectsConnectCompletionContradictions(t *testing.T) {
	for _, corruption := range []string{"none", "scope", "caller", "client", "revision", "time", "action"} {
		t.Run(corruption, func(t *testing.T) {
			committed := time.Now().UTC()
			audit := connect.Audit{Action: "client_configuration.apply", Caller: "local", Workspace: "research", Client: "codex", Revision: "connect-" + strings.Repeat("a", 64), CommittedAt: committed}
			event := managementaudit.Event{Action: audit.Action, Actor: audit.Caller, Workspace: audit.Workspace, CommittedAt: committed, ClientConfiguration: &audit}
			switch corruption {
			case "scope":
				audit.Workspace = "foreign"
			case "caller":
				audit.Caller = "forged"
			case "client":
				audit.Client = "unknown"
			case "revision":
				audit.Revision = "connect-invalid"
			case "time":
				audit.CommittedAt = committed.Add(time.Second)
			case "action":
				audit.Action = "other"
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				json.NewEncoder(writer).Encode(managementaudit.Snapshot{Workspace: "research", Coverage: "partial", CaptureFamilies: []string{"checked_permission", "approved_limit", "checked_principal_revocation", "checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"}, Events: []managementaudit.Event{event}})
			}))
			defer server.Close()
			result, err := New(server.Client(), server.URL).AuditEvents(t.Context(), "research")
			if (err == nil) != (corruption == "none") || (err != nil && result.Events != nil) {
				t.Fatal("Connect audit contradiction returned evidence", err)
			}
		})
	}
}
