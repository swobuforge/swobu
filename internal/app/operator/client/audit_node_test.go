package operatorclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/app/operator/managementaudit"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
)

func TestNodeAuditEventsKeepsScopeFreeActionsAndRejectsWrongBindings(t *testing.T) {
	for _, corruption := range []string{"none", "snapshot_scope", "event_scope", "null_scope", "foreign_actor"} {
		t.Run(corruption, func(t *testing.T) {
			committed := time.Now().UTC()
			audit := &accessstate.PrincipalRevocationAudit{Principal: accessidentity.NewManaged([16]byte{1}).String(), Caller: "local", Revision: "review", Workspaces: []string{}, CommittedAt: committed}
			snapshot := managementaudit.Snapshot{Coverage: "partial", CaptureFamilies: []string{"checked_permission", "approved_limit", "checked_principal_revocation", "checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"}, Events: []managementaudit.Event{{Action: "principal.revoke", Actor: "local", CommittedAt: committed, PrincipalRevocation: audit}}}
			switch corruption {
			case "snapshot_scope":
				snapshot.Workspace = "research"
			case "event_scope":
				snapshot.Events[0].Workspace = "research"
			case "null_scope":
				audit.Workspaces = nil
			case "foreign_actor":
				snapshot.Events[0].Actor = "foreign"
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				if request.URL.Path != "/api/v1/audit/events" || request.URL.RawQuery != "" || request.Method != http.MethodGet {
					t.Error("Node audit escaped canonical scope")
				}
				json.NewEncoder(writer).Encode(snapshot)
			}))
			defer server.Close()
			client := New(server.Client(), server.URL)
			if _, err := client.AuditEvents(t.Context(), ""); err == nil || calls != 0 {
				t.Fatal("empty Workspace implicitly widened scope")
			}
			_, err := client.NodeAuditEvents(t.Context())
			if calls != 1 || (err == nil) != (corruption == "none") {
				t.Fatal("Node audit acknowledgement misclassified", err)
			}
		})
	}
}
