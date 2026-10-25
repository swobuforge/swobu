package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/swobuforge/swobu/internal/accessstate"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	operatorclient "github.com/swobuforge/swobu/internal/app/operator/client"
	"github.com/swobuforge/swobu/internal/app/operator/managementaudit"
	"github.com/swobuforge/swobu/internal/platform/config"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestDaemonLifecycleAuditRetainsOriginalFactsAfterReplacementAndRestart(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	input := StartInput{ConfigPath: filepath.Join(root, "swobu.yaml"), StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}}
	daemon, err := Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { daemon.Close() }()
	slug, _ := routing.ParseWorkspaceSlug("research")
	draft, err := routing.NewWorkspace(slug, nil)
	if err != nil {
		t.Fatal(err)
	}
	admit := func(context.Context) error { return nil }
	original, _, err := daemon.configStore.CreateWorkspaceChecked(t.Context(), draft, "local:POST:workspace", "original-key", admit)
	if err != nil {
		t.Fatal(err)
	}
	etag := fmt.Sprintf("%s-%d", original.ManagementIncarnation(), original.ManagementRevision())
	updated, err := daemon.configStore.SetWorkspaceDisplayNameChecked(t.Context(), slug, "Research", etag, admit)
	if err != nil {
		t.Fatal(err)
	}
	displayETag := etag
	etag = fmt.Sprintf("%s-%d", updated.ManagementIncarnation(), updated.ManagementRevision())
	if _, err := daemon.configStore.DeleteWorkspaceChecked(t.Context(), slug, etag, "local:DELETE:workspace", "deletion-key", admit, nil); err != nil {
		t.Fatal(err)
	}
	replacement, _, err := daemon.configStore.CreateWorkspaceChecked(t.Context(), draft, "local:POST:workspace", "replacement-key", admit)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}
	daemon, err = Start(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(input.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if replayed, err := daemon.configStore.DeleteWorkspaceChecked(t.Context(), slug, etag, "local:DELETE:workspace", "deletion-key", admit, nil); err != nil || !replayed {
		t.Fatal("historical deletion failed", err)
	}
	api := operatorclient.New(http.DefaultClient, daemon.BaseURL())
	for _, scope := range []string{"research", "foreign", ""} {
		var snapshot managementaudit.Snapshot
		if scope == "" {
			snapshot, err = api.NodeAuditEvents(t.Context())
		} else {
			snapshot, err = api.AuditEvents(t.Context(), scope)
		}
		if err != nil {
			t.Fatal(err)
		}
		expected := 4
		if scope == "foreign" {
			expected = 0
		}
		if len(snapshot.Events) != expected || snapshot.Coverage != "partial" {
			t.Fatal("lifecycle audit scope or capture incorrect", scope, snapshot)
		}
		creations, deletions, displayNames := 0, 0, 0
		for _, event := range snapshot.Events {
			if audit := event.WorkspaceDisplayName; audit != nil {
				displayNames++
				if audit.ETag != displayETag || audit.Before != "" || audit.After != "Research" {
					t.Fatal("display history adopted replacement metadata")
				}
			}
			if audit := event.WorkspaceCreation; audit != nil {
				creations++
				if audit.Incarnation != original.ManagementIncarnation() && audit.Incarnation != replacement.ManagementIncarnation() {
					t.Fatal("creation lost lifetime")
				}
			}
			if audit := event.WorkspaceDeletion; audit != nil {
				deletions++
				if audit.ETag != etag {
					t.Fatal("deletion adopted replacement lifetime")
				}
			}
		}
		if expected > 0 && (creations != 2 || deletions != 1 || displayNames != 1) {
			t.Fatal("lifecycle audit duplicated replay")
		}
		raw, err := json.Marshal(snapshot)
		if err != nil || bytes.Contains(raw, []byte("original-key")) || bytes.Contains(raw, []byte("deletion-key")) || bytes.Contains(raw, []byte("replacement-key")) || bytes.Contains(raw, []byte("intent_hash")) {
			t.Fatal("audit exposed retry authority", err)
		}
	}
	after, err := os.ReadFile(input.ConfigPath)
	current, currentErr := daemon.configStore.GetWorkspace(t.Context(), slug)
	if err != nil || currentErr != nil || !bytes.Equal(before, after) || current.ManagementIncarnation() != replacement.ManagementIncarnation() {
		t.Fatal("audit read/replay changed replacement")
	}
}

func TestDaemonAuditReadsCommittedFactsAndDeniesWorkspaceCredential(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "swobu.yaml")
	seedTestWorkspaces(t, configPath, "research")
	daemon, err := Start(context.Background(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	workspace, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	principal, issued, err := daemon.accessStore.Create("operator", workspace)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := daemon.accessStore.PreviewPermission(principal.ID, workspace, "", true, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.accessStore.ApplyPermissionChecked(t.Context(), principal.ID, workspace, preview.Revision, "audit-key", true, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	client := operatorclient.New(&http.Client{}, daemon.BaseURL())
	snapshot, err := client.AuditEvents(t.Context(), "research")
	if err != nil || len(snapshot.Events) != 1 || snapshot.Events[0].Permission.Principal != principal.ID.String() {
		t.Fatal("canonical audit mount lost owner facts", err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, daemon.BaseURL()+"/api/v1/audit/events?workspace=research", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+issued.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatal("Workspace credential reached private audit", response.StatusCode)
	}
	request.URL.RawQuery = ""
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatal("valid Workspace credential reached Node audit", response.StatusCode)
	}
	identity := daemon.accessStore.List()[0]
	credentialReview, err := daemon.accessStore.PreviewCredentialRevocation(identity.ID, issued.Credential.ID, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := daemon.accessStore.ApplyCredentialRevocation(t.Context(), identity.ID, issued.Credential.ID, credentialReview.Revision, "credential-audit-key", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := daemon.accessStore.ApplyPrincipalRevocation(identity.ID, accessstate.PrincipalRevision(daemon.accessStore.List()[0]), "revoke-audit-key", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	snapshot, err = client.AuditEvents(t.Context(), "research")
	if err != nil || len(snapshot.Events) != 3 || snapshot.Events[1].CredentialRevocation == nil || snapshot.Events[1].CredentialRevocation.Credential != issued.Credential.ID.String() || snapshot.Events[2].PrincipalRevocation == nil || snapshot.Events[2].PrincipalRevocation.Principal != identity.ID.String() {
		t.Fatal("Principal revocation audit missing", err)
	}
	if _, err := client.AuditEvents(t.Context(), "research"); err != nil {
		t.Fatal("fresh local read did not recover", err)
	}
	node, err := client.NodeAuditEvents(t.Context())
	if err != nil || len(node.Events) != 3 || node.Workspace != "" || node.Events[2].Workspace != "" {
		t.Fatal("Node audit lost or misattributed revocation", err)
	}
	request, err = http.NewRequestWithContext(t.Context(), http.MethodGet, daemon.BaseURL()+"/api/v1/audit/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+issued.Token)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("Workspace credential reached Node audit")
	}
}

func TestDaemonAuditReadsPrincipalCreationOnlyAtNodeScope(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "swobu.yaml")
	seedTestWorkspaces(t, configPath, "research")
	daemon, err := Start(context.Background(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	admit := func() error { return nil }
	identity, _, err := daemon.accessStore.CreatePrincipalChecked(t.Context(), "original", "creation-audit-key", admit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := daemon.accessStore.UpdatePrincipalLabelChecked(t.Context(), identity.ID, "later", accessstate.PrincipalRevision(identity), admit); err != nil {
		t.Fatal(err)
	}
	client := operatorclient.New(&http.Client{}, daemon.BaseURL())
	node, err := client.NodeAuditEvents(t.Context())
	if err != nil || len(node.Events) != 2 || node.Events[0].PrincipalCreation == nil || node.Events[0].PrincipalCreation.Label != "original" || node.Events[0].Workspace != "" || node.Events[1].PrincipalLabel == nil || node.Events[1].PrincipalLabel.Before != "original" || node.Events[1].PrincipalLabel.After != "later" {
		t.Fatal("Node creation audit lost original label", err)
	}
	scoped, err := client.AuditEvents(t.Context(), "research")
	if err != nil || len(scoped.Events) != 0 {
		t.Fatal("creation acquired invented Workspace impact", err)
	}
}

func TestDaemonAuditReadsIssuanceWithoutRecoveringSecret(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "swobu.yaml")
	seedTestWorkspaces(t, configPath, "research")
	daemon, err := Start(context.Background(), StartInput{ConfigPath: configPath, StartupConfig: config.StartupConfig{Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer daemon.Close()
	workspace, err := routing.ParseWorkspaceSlug("research")
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := daemon.accessStore.Create("operator", workspace)
	if err != nil {
		t.Fatal(err)
	}
	issued, _, err := daemon.accessStore.IssueCredentialChecked(identity.ID, workspace, "issuance-audit-key", func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	client := operatorclient.New(&http.Client{}, daemon.BaseURL())
	for _, nodeScope := range []bool{false, true} {
		var snapshot managementaudit.Snapshot
		if nodeScope {
			snapshot, err = client.NodeAuditEvents(t.Context())
		} else {
			snapshot, err = client.AuditEvents(t.Context(), "research")
		}
		if err != nil || len(snapshot.Events) != 1 || snapshot.Events[0].CredentialIssuance == nil || snapshot.Events[0].CredentialIssuance.Credential != issued.Credential.ID.String() {
			t.Fatal("issuance audit lost original metadata", err)
		}
		body, err := json.Marshal(snapshot)
		if err != nil || bytes.Contains(body, []byte(issued.Token)) || bytes.Contains(body, []byte("issuance-audit-key")) || bytes.Contains(body, []byte("key_hash")) || bytes.Contains(body, []byte("intent_hash")) {
			t.Fatal("audit disclosed issuance authority", err)
		}
	}
}
