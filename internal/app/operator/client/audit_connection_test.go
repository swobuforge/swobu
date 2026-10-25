package operatorclient

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/managementaudit"
	"github.com/swobuforge/swobu/internal/configstore"
	"github.com/swobuforge/swobu/internal/connections"
)

func TestConnectionAuditClientValidatesClosedFactsAndNodeScope(t *testing.T) {
	for _, action := range []string{"connection.create", "connection.edit", "connection.credential_replace", "connection.delete"} {
		for _, corruption := range []string{"none", "caller", "scope", "workspace-read", "action", "time", "revision", "missing", "null", "unknown", "duplicate", "mixed"} {
			t.Run(action+"/"+corruption, func(t *testing.T) {
				now := time.Now().UTC()
				fact := connections.Audit{Action: action, Caller: "local", ConnectionID: "cn_fixture", CommittedAt: now}
				switch action {
				case "connection.create":
					fact.AfterRevision = "1"
				case "connection.edit", "connection.credential_replace":
					fact.BeforeRevision = "1"
					fact.AfterRevision = "2"
				case "connection.delete":
					fact.BeforeRevision = "2"
				}
				event := managementaudit.Event{Action: action, Actor: "local", CommittedAt: now, Connection: &fact}
				scope := ""
				switch corruption {
				case "caller":
					fact.Caller = "forged"
				case "scope":
					event.Workspace = "research"
				case "workspace-read":
					scope = "research"
					event.Workspace = scope
				case "action":
					fact.Action = "other"
				case "time":
					fact.CommittedAt = now.Add(time.Second)
				case "revision":
					fact.BeforeRevision = "0"
					fact.AfterRevision = "0"
				case "mixed":
					event.WorkspaceCreation = &configstore.WorkspaceCreationAudit{}
				}
				snapshot := managementaudit.Snapshot{Workspace: scope, Coverage: "partial", CaptureFamilies: []string{"checked_permission", "approved_limit", "checked_principal_revocation", "checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"}, Events: []managementaudit.Event{event}}
				raw, err := json.Marshal(snapshot)
				if err != nil {
					t.Fatal(err)
				}
				switch corruption {
				case "missing":
					raw = bytes.Replace(raw, []byte(`"connection_id":"cn_fixture",`), nil, 1)
				case "null":
					raw = bytes.Replace(raw, []byte(`"connection_id":"cn_fixture"`), []byte(`"connection_id":null`), 1)
				case "unknown":
					raw = bytes.Replace(raw, []byte(`"connection_id":"cn_fixture"`), []byte(`"connection_id":"cn_fixture","secret":"private"`), 1)
				case "duplicate":
					raw = bytes.Replace(raw, []byte(`"connection_id":"cn_fixture"`), []byte(`"connection_id":"other","connection_id":"cn_fixture"`), 1)
				}
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(raw) }))
				defer server.Close()
				client := New(server.Client(), server.URL)
				if scope == "" {
					_, err = client.NodeAuditEvents(t.Context())
				} else {
					_, err = client.AuditEvents(t.Context(), scope)
				}
				if (err == nil) != (corruption == "none") {
					t.Fatalf("classification: %v", err)
				}
			})
		}
	}
}
