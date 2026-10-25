package operatorclient

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/app/operator/managementaudit"
	"github.com/swobuforge/swobu/internal/configstore"
)

func TestAuditDisplayNameRejectsContradictionsAndMissingEmptyLabelEvidence(t *testing.T) {
	for _, corruption := range []string{"none", "scope", "caller", "revision", "time", "action", "noop", "noncanonical", "missing_before", "null_before", "mixed"} {
		t.Run(corruption, func(t *testing.T) {
			committed := time.Now().UTC()
			audit := configstore.WorkspaceDisplayNameAudit{Action: "workspace.display_name", Caller: "local", Workspace: "research", ETag: strings.Repeat("a", 64) + "-1", Before: "", After: "Research", CommittedAt: committed}
			event := managementaudit.Event{Action: audit.Action, Actor: audit.Caller, Workspace: audit.Workspace, CommittedAt: committed, WorkspaceDisplayName: &audit}
			switch corruption {
			case "scope":
				audit.Workspace = "foreign"
			case "caller":
				audit.Caller = "forged"
			case "revision":
				audit.ETag = "invalid"
			case "time":
				audit.CommittedAt = committed.Add(time.Second)
			case "action":
				audit.Action = "other"
			case "noop":
				audit.After = ""
			case "noncanonical":
				audit.After = " Research "
			case "mixed":
				event.WorkspaceCreation = &configstore.WorkspaceCreationAudit{Action: "workspace.create"}
			}
			snapshot := managementaudit.Snapshot{Workspace: "research", Coverage: "partial", CaptureFamilies: []string{"checked_permission", "approved_limit", "checked_principal_revocation", "checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"}, Events: []managementaudit.Event{event}}
			raw, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if corruption == "missing_before" {
				raw = bytes.Replace(raw, []byte(`"before":"",`), nil, 1)
			}
			if corruption == "null_before" {
				raw = bytes.Replace(raw, []byte(`"before":""`), []byte(`"before":null`), 1)
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { calls++; writer.Write(raw) }))
			defer server.Close()
			result, err := New(server.Client(), server.URL).AuditEvents(t.Context(), "research")
			if calls != 1 || (err == nil) != (corruption == "none") || (err != nil && result.Events != nil) {
				t.Fatal("invalid display facts returned evidence", err)
			}
		})
	}
}
