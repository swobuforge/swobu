package operatorclient

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/app/operator/managementaudit"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
)

func TestAuditCredentialRevocationRejectsContradictoryBindings(t *testing.T) {
	for _, corruption := range []string{"none", "principal", "credential", "scope", "caller", "action", "created", "committed", "revision"} {
		t.Run(corruption, func(t *testing.T) {
			committed := time.Now().UTC()
			audit := &accessstate.CredentialRevocationAudit{Principal: accessidentity.NewManaged([16]byte{1}).String(), Credential: "acred_" + base64.RawURLEncoding.EncodeToString(make([]byte, 16)), Workspace: "research", CreatedAt: committed.Add(-time.Hour), Caller: "local", Revision: "review", CommittedAt: committed}
			event := managementaudit.Event{Action: "credential.revoke", Actor: "local", Workspace: "research", CommittedAt: committed, CredentialRevocation: audit}
			switch corruption {
			case "principal":
				audit.Principal = "local"
			case "credential":
				audit.Credential = "invalid"
			case "scope":
				audit.Workspace = "coding"
			case "caller":
				audit.Caller = "foreign"
			case "action":
				event.Action = "principal.revoke"
			case "created":
				audit.CreatedAt = time.Time{}
			case "committed":
				audit.CommittedAt = committed.Add(time.Second)
			case "revision":
				audit.Revision = ""
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				json.NewEncoder(writer).Encode(managementaudit.Snapshot{Workspace: "research", Coverage: "partial", CaptureFamilies: []string{"checked_permission", "approved_limit", "checked_principal_revocation", "checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"}, Events: []managementaudit.Event{event}})
			}))
			defer server.Close()
			_, err := New(server.Client(), server.URL).AuditEvents(t.Context(), "research")
			if (err == nil) != (corruption == "none") {
				t.Fatal("credential audit misclassified", err)
			}
		})
	}
}
