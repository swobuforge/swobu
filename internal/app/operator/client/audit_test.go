package operatorclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/accounting"
	"github.com/swobuforge/swobu/internal/app/operator/managementaudit"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
)

func TestAuditEventsRequiresExactScopeAndCoverageWithoutRetry(t *testing.T) {
	for _, body := range []string{
		`{"workspace":"research","coverage":"partial","capture_families":["checked_permission","approved_limit","checked_principal_revocation","checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"],"events":[]}`,
		`{"workspace":"coding","coverage":"partial","capture_families":["checked_permission","approved_limit","checked_principal_revocation","checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"],"events":[]}`,
		`{"workspace":"research","coverage":"complete","capture_families":["checked_permission","approved_limit","checked_principal_revocation","checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"],"events":[]}`,
		`{"workspace":"research","coverage":"partial","capture_families":["checked_permission","approved_limit","checked_principal_revocation","checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"],"events":null}`,
		`{"workspace":"research","coverage":"partial","capture_families":[],"events":[]}`,
		`{"workspace":"research","coverage":"partial","capture_families":["checked_permission","approved_limit","checked_principal_revocation","checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"],"events":[]} {}`,
	} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			calls++
			if request.Method != http.MethodGet || request.URL.Path != "/api/v1/audit/events" || request.URL.RawQuery != "workspace=research" {
				t.Error("audit escaped canonical scope")
			}
			fmt.Fprint(writer, body)
		}))
		client := New(server.Client(), server.URL)
		if _, err := client.AuditEvents(t.Context(), "Research"); err == nil || calls != 0 {
			t.Fatal("invalid scope contacted Node")
		}
		_, err := client.AuditEvents(t.Context(), "research")
		server.Close()
		if calls != 1 || (err == nil) != (body == `{"workspace":"research","coverage":"partial","capture_families":["checked_permission","approved_limit","checked_principal_revocation","checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"],"events":[]}`) {
			t.Fatal("invalid acknowledgement or retry", err)
		}
	}
}

func TestAuditEventsRejectsPrincipalRevocationContradictions(t *testing.T) {
	for _, corruption := range []string{"none", "principal", "caller", "revision", "time", "scope", "duplicate", "action"} {
		t.Run(corruption, func(t *testing.T) {
			committed := time.Now().UTC()
			audit := &accessstate.PrincipalRevocationAudit{Principal: accessidentity.NewManaged([16]byte{1}).String(), Caller: "local", Revision: "review", Workspaces: []string{"research"}, CommittedAt: committed}
			event := managementaudit.Event{Action: "principal.revoke", Actor: "local", Workspace: "research", CommittedAt: committed, PrincipalRevocation: audit}
			switch corruption {
			case "principal":
				audit.Principal = "local"
			case "caller":
				audit.Caller = "foreign"
			case "revision":
				audit.Revision = ""
			case "time":
				audit.CommittedAt = committed.Add(time.Second)
			case "scope":
				audit.Workspaces = []string{"coding"}
			case "duplicate":
				audit.Workspaces = []string{"research", "research"}
			case "action":
				event.Action = "permission.revoke"
			}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				json.NewEncoder(writer).Encode(managementaudit.Snapshot{Workspace: "research", Coverage: "partial", CaptureFamilies: []string{"checked_permission", "approved_limit", "checked_principal_revocation", "checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"}, Events: []managementaudit.Event{event}})
			}))
			defer server.Close()
			_, err := New(server.Client(), server.URL).AuditEvents(t.Context(), "research")
			if (err == nil) != (corruption == "none") {
				t.Fatal("revocation acknowledgement misclassified", err)
			}
		})
	}
}

func TestAuditEventsRejectsContradictoryFactsAndPrivateDenial(t *testing.T) {
	for _, corruption := range []string{"none", "scope", "action", "actor", "principal", "revision", "time", "expiry", "both", "neither", "denial"} {
		t.Run(corruption, func(t *testing.T) {
			committed := time.Now().UTC()
			permission := &accessstate.PermissionAudit{Principal: "local", Workspace: "research", Action: "permission.revoke", Revision: "review", CommittedAt: committed}
			event := managementaudit.Event{Action: permission.Action, Actor: "local", Workspace: "research", CommittedAt: committed, Permission: permission}
			switch corruption {
			case "scope":
				permission.Workspace = "coding"
			case "action":
				event.Action = "permission.grant"
			case "actor":
				event.Actor = "foreign"
			case "principal":
				permission.Principal = "invalid"
			case "revision":
				permission.Revision = ""
			case "time":
				permission.CommittedAt = committed.Add(time.Second)
			case "expiry":
				permission.ExpiresAt = committed
			case "both":
				event.Limit = &accounting.LimitChange{}
			case "neither":
				event.Permission = nil
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				if corruption == "denial" {
					writer.WriteHeader(http.StatusForbidden)
					fmt.Fprint(writer, `{"message":"private-denial"}`)
					return
				}
				json.NewEncoder(writer).Encode(managementaudit.Snapshot{Workspace: "research", Coverage: "partial", CaptureFamilies: []string{"checked_permission", "approved_limit", "checked_principal_revocation", "checked_credential_revocation", "checked_credential_issuance", "checked_principal_creation", "checked_principal_label", "verified_client_configuration", "checked_workspace_creation", "checked_workspace_deletion", "checked_workspace_display_name", "checked_connection"}, Events: []managementaudit.Event{event}})
			}))
			defer server.Close()
			result, err := New(server.Client(), server.URL).AuditEvents(t.Context(), "research")
			if calls != 1 || (err == nil) != (corruption == "none") {
				t.Fatal("audit binding accepted or retried", err)
			}
			if err != nil && (len(result.Events) != 0 || strings.Contains(err.Error(), "private-denial")) {
				t.Fatal("partial or private denial leaked")
			}
		})
	}
}
