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

func TestNodeAuditPrincipalLabelRejectsContradictoryFacts(t *testing.T) {
	for _, corruption := range []string{"none", "principal", "caller", "before", "after", "revision", "time", "scope", "action"} {
		t.Run(corruption, func(t *testing.T) {
			committed := time.Now().UTC()
			principal := accessidentity.NewManaged([16]byte{1})
			audit := &accessstate.PrincipalLabelAudit{Principal: principal.String(), Caller: "local", Before: "original", After: "later", Generation: 1, Revision: accessstate.PrincipalRevision(accessstate.Access{ID: principal, AuthorityGeneration: 1}), CommittedAt: committed}
			event := managementaudit.Event{Action: "principal.label", Actor: "local", CommittedAt: committed, PrincipalLabel: audit}
			switch corruption {
			case "principal":
				audit.Principal = "local"
			case "caller":
				audit.Caller = "foreign"
			case "before":
				audit.Before = ""
			case "after":
				audit.After = audit.Before
			case "revision":
				audit.Revision = "wrong"
			case "time":
				audit.CommittedAt = committed.Add(time.Second)
			case "scope":
				event.Workspace = "research"
			case "action":
				event.Action = "principal.revoke"
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				json.NewEncoder(writer).Encode(managementaudit.Snapshot{Coverage: "partial", CaptureFamilies: []string{"checked_permission", "approved_limit", "checked_principal_revocation", "checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"}, Events: []managementaudit.Event{event}})
			}))
			defer server.Close()
			_, err := New(server.Client(), server.URL).NodeAuditEvents(t.Context())
			if (err == nil) != (corruption == "none") {
				t.Fatal("label facts misclassified", err)
			}
		})
	}
}
