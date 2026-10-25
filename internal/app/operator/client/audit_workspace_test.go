package operatorclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/managementaudit"
	"github.com/swobuforge/swobu/internal/configstore"
)

func TestAuditEventsRejectsWorkspaceLifecycleContradictions(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		for _, corruption := range []string{"none", "scope", "caller", "identity", "time", "action", "multiple"} {
			t.Run(corruption+map[bool]string{false: "/create", true: "/delete"}[deletion], func(t *testing.T) {
				committed := time.Now().UTC()
				creation := configstore.WorkspaceCreationAudit{Action: "workspace.create", Caller: "local:POST:workspace", Workspace: "research", Incarnation: strings.Repeat("a", 64), CommittedAt: committed}
				removal := configstore.WorkspaceDeletionAudit{Action: "workspace.delete", Caller: "local:DELETE:workspace", Workspace: "research", ETag: strings.Repeat("a", 64) + "-1", CommittedAt: committed}
				event := managementaudit.Event{Action: creation.Action, Actor: creation.Caller, Workspace: "research", CommittedAt: committed, WorkspaceCreation: &creation}
				if deletion {
					event.Action, event.Actor, event.WorkspaceCreation, event.WorkspaceDeletion = removal.Action, removal.Caller, nil, &removal
				}
				switch corruption {
				case "scope":
					creation.Workspace, removal.Workspace = "foreign", "foreign"
				case "caller":
					creation.Caller, removal.Caller = "forged", "forged"
				case "identity":
					creation.Incarnation, removal.ETag = "invalid", "invalid"
				case "time":
					creation.CommittedAt, removal.CommittedAt = committed.Add(time.Second), committed.Add(time.Second)
				case "action":
					creation.Action, removal.Action = "other", "other"
				case "multiple":
					event.WorkspaceCreation, event.WorkspaceDeletion = &creation, &removal
				}
				server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					json.NewEncoder(writer).Encode(managementaudit.Snapshot{Workspace: "research", Coverage: "partial", CaptureFamilies: []string{"checked_permission", "approved_limit", "checked_principal_revocation", "checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"}, Events: []managementaudit.Event{event}})
				}))
				defer server.Close()
				result, err := New(server.Client(), server.URL).AuditEvents(t.Context(), "research")
				if (err == nil) != (corruption == "none") || (err != nil && result.Events != nil) {
					t.Fatal("invalid lifecycle facts returned evidence", err)
				}
			})
		}
	}
}
