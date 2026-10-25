package operatorclient

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	workspaceapi "github.com/swobuforge/swobu/internal/app/operator/workspaces"
)

func TestWorkspaceCreationRecoveryPreservesOriginalRequestWithoutSending(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "configured"}[configured], func(t *testing.T) {
			command := workspaceapi.CreateWorkspace{Slug: "research", DisplayName: "Research 東京"}
			if configured {
				command.InitialRoute = "chat"
				command.Target = workspaceapi.TargetDraft{ID: "original-generated-id", Model: "original-model", Protocol: "responses", ConnectionID: "cn_original"}
			}
			original, err := NewWorkspaceCreationIntent(command, "original-key")
			if err != nil {
				t.Fatal(err)
			}
			record, err := original.RecoveryRecord()
			if err != nil {
				t.Fatal(err)
			}
			restored, err := RestoreWorkspaceCreationIntent(record)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := restored.RecoveryIdentity()
			if err != nil || identity.Slug != command.Slug || identity.DisplayName != command.DisplayName || identity.Route != command.InitialRoute || identity.TargetID != command.Target.ID || identity.RetryKey != "original-key" {
				t.Fatal("restored attribution changed", err)
			}
			record[0] = 'x'
			command.Target.ID, command.DisplayName = "changed", "Changed"
			calls := 0
			client := New(&http.Client{Transport: principalCreationTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				raw, _ := io.ReadAll(request.Body)
				if !bytes.Equal(raw, original.body) || request.Header.Get("Idempotency-Key") != original.key || request.GetBody != nil {
					t.Fatal("restored send changed original request")
				}
				return &http.Response{StatusCode: 409, Body: io.NopCloser(strings.NewReader(`{"code":"CONFLICT","message":"fixture conflict"}`)), Header: make(http.Header)}, nil
			})}, "http://fixture")
			if calls != 0 {
				t.Fatal("restore sent HTTP")
			}
			if _, _, err := client.ApplyWorkspaceCreation(t.Context(), restored); err == nil || calls != 1 {
				t.Fatal("explicit restored apply must send once and preserve failure")
			}
		})
	}
}

func TestWorkspaceCreationRecoveryRejectsNoncanonicalOrMalformedRecords(t *testing.T) {
	intent, err := NewWorkspaceCreationIntent(workspaceapi.CreateWorkspace{Slug: "research"}, "original-key")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := intent.RecoveryRecord()
	if err != nil {
		t.Fatal(err)
	}
	valid := string(raw)
	for _, record := range []string{
		"", "null", "{}", valid + "{}", " " + valid,
		strings.Replace(valid, `"version":1`, `"version":2`, 1),
		strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1),
		strings.Replace(valid, `"version":1`, `"version":1,"approved":true`, 1),
		strings.Replace(valid, `"original-key"`, `""`, 1),
		strings.Replace(valid, `"research"`, `"bad/slug"`, 1),
		strings.Replace(valid, `"slug":"research"`, `"slug":"research","slug":"research"`, 1),
		strings.Replace(valid, `"slug":"research"`, `"slug":"research","target":null`, 1),
		strings.Replace(valid, `"slug":"research"`, `"slug":"research","approval":true`, 1),
	} {
		if restored, err := RestoreWorkspaceCreationIntent([]byte(record)); err == nil || len(restored.body) != 0 {
			t.Fatal("invalid recovery accepted")
		}
	}
	if _, err := (WorkspaceCreationIntent{}).RecoveryRecord(); err == nil {
		t.Fatal("zero intent exported")
	}
	if _, err := (WorkspaceCreationIntent{}).RecoveryIdentity(); err == nil {
		t.Fatal("zero intent acquired attribution")
	}
}

func TestWorkspaceCreationRecoveryRejectsInvalidConfiguredTarget(t *testing.T) {
	for _, target := range []workspaceapi.TargetDraft{
		{ID: "", Model: "model", Protocol: "responses", ConnectionID: "cn_original"},
		{ID: "target", Model: "", Protocol: "responses", ConnectionID: "cn_original"},
		{ID: "target", Model: "model", Protocol: "", ConnectionID: "cn_original"},
		{ID: "target", Model: "model", Protocol: "responses", ConnectionID: "invalid/connection"},
		{ID: "target", Model: "model", Protocol: "responses", ConnectionID: ""},
	} {
		// The wire-intent constructor can freeze invalid semantic drafts; only
		// domain validation may establish whether durable recovery is safe.
		intent, err := NewWorkspaceCreationIntent(workspaceapi.CreateWorkspace{Slug: "research", InitialRoute: "chat", Target: target}, "original-key")
		if err != nil {
			t.Fatal(err)
		}
		record, err := intent.RecoveryRecord()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := RestoreWorkspaceCreationIntent(record); err == nil || err.Error() != "invalid Workspace creation recovery record" {
			t.Fatal("invalid target recovered or diagnostics exposed request data")
		}
	}
}
