package operatorclient

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
)

type permissionFailureTransport struct{ calls int }

func TestPermissionClientRequiresExplicitMutationDecisionFields(t *testing.T) {
	principal := accessidentity.NewManaged([16]byte{1}).String()
	reviewed, err := ReviewPermission(accessapp.PermissionPreview{Principal: principal, Workspace: "research", Operation: "revoke", Revision: "reviewed", ApprovalClass: "trusted_confirmation", Warning: "confirm"}, "explicit-result")
	if err != nil {
		t.Fatal(err)
	}
	for _, fields := range []string{
		`"permission":null,"allowed":false`,
		`"permission":null,"replayed":false`,
		`"allowed":false,"replayed":false`,
		`"permission":null,"allowed":null,"replayed":false`,
		`"permission":null,"allowed":false,"replayed":null`,
		`"permission":null,"allowed":"false","replayed":false`,
		`"permission":null,"allowed":false,"replayed":0`,
	} {
		t.Run(fields, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = w.Write([]byte(`{"principal":"` + principal + `","workspace":"research","revision":"new",` + fields + `}`))
			}))
			defer server.Close()
			client := New(server.Client(), server.URL)
			if _, err := client.ApplyWorkspacePermission(t.Context(), reviewed); err == nil {
				t.Fatal("incomplete decision reported success")
			}
			if calls != 1 {
				t.Fatal("incomplete response caused retry")
			}
		})
	}
}

func TestPermissionClientAcceptsExplicitAbsentReplayWithAdditiveMetadata(t *testing.T) {
	principal := accessidentity.NewManaged([16]byte{1}).String()
	reviewed, err := ReviewPermission(accessapp.PermissionPreview{Principal: principal, Workspace: "research", Operation: "grant", Revision: "reviewed", ApprovalClass: "trusted_confirmation", Warning: "confirm", After: &accessapp.Permission{Workspace: "research", Action: "use"}}, "explicit-replay")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"principal":"` + principal + `","workspace":"research","revision":"current","permission":null,"allowed":false,"replayed":true,"future_metadata":{"note":"additive"}}`))
	}))
	defer server.Close()
	client := New(server.Client(), server.URL)
	result, err := client.ApplyWorkspacePermission(t.Context(), reviewed)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Replayed || result.Allowed || result.Permission != nil || calls != 1 {
		t.Fatal("explicit absent replay lost meaning")
	}
}

func TestPermissionClientRejectsContradictoryMutationResultWithoutRetry(t *testing.T) {
	principal := accessidentity.NewManaged([16]byte{1}).String()
	preview := accessapp.PermissionPreview{Principal: principal, Workspace: "research", Operation: "grant", Revision: "reviewed", ApprovalClass: "trusted_confirmation", Warning: "confirm", After: &accessapp.Permission{Workspace: "research", Action: "use"}}
	reviewed, err := ReviewPermission(preview, "result-key")
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []accessapp.PermissionMutation{
		{PermissionState: accessapp.PermissionState{Principal: principal, Workspace: "other", Revision: "new"}},
		{PermissionState: accessapp.PermissionState{Principal: principal, Workspace: "research", Revision: "new", Allowed: true}},
		{PermissionState: accessapp.PermissionState{Principal: principal, Workspace: "research", Revision: "new", Permission: &accessapp.Permission{Workspace: "research", Action: "manage"}}},
		{PermissionState: accessapp.PermissionState{Principal: principal, Workspace: "research", Revision: "bad revision", Permission: preview.After}},
	} {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			_ = json.NewEncoder(w).Encode(result)
		}))
		client := New(server.Client(), server.URL)
		if _, err := client.ApplyWorkspacePermission(t.Context(), reviewed); err == nil {
			t.Fatal("contradictory result reported success")
		}
		if calls != 1 {
			t.Fatal("malformed result caused retry or discovery")
		}
		server.Close()
	}
}

func (transport *permissionFailureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls++
	return nil, errors.New("response lost")
}

func TestPermissionClientTransportFailureDoesNotRetryOrRefresh(t *testing.T) {
	principal := accessidentity.NewManaged([16]byte{1}).String()
	reviewed, err := ReviewPermission(accessapp.PermissionPreview{Principal: principal, Workspace: "research", Operation: "revoke", Revision: "reviewed", ApprovalClass: "trusted_confirmation", Warning: "confirm"}, "stable-key")
	if err != nil {
		t.Fatal(err)
	}
	transport := &permissionFailureTransport{}
	client := New(&http.Client{Transport: transport}, "http://127.0.0.1:7926")
	if _, err := client.ApplyWorkspacePermission(t.Context(), reviewed); err == nil {
		t.Fatal("lost response reported success")
	}
	if transport.calls != 1 {
		t.Fatal("transport failure caused discovery or automatic retry")
	}
}

func TestPermissionClientRetainsExactPreviewAndRetryIntent(t *testing.T) {
	principal := accessidentity.NewManaged([16]byte{1}).String()
	deadline := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, grant := range []bool{true, false} {
		operation, method := "revoke", http.MethodDelete
		if grant {
			operation, method = "grant", http.MethodPut
		}
		preview := accessapp.PermissionPreview{Principal: principal, Workspace: "research", Operation: operation, Revision: "exact-revision", ApprovalClass: "trusted_confirmation", Warning: "Review impact", AffectedCredentials: 2}
		if grant {
			value := deadline
			preview.After = &accessapp.Permission{Workspace: "research", Action: "use", ExpiresAt: &value}
		}
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			if r.Method != method || r.URL.Path != "/api/v1/workspaces/research/permissions/"+principal {
				t.Error("wrong method/resource")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if grant && (body["action"] != "use" || body["expires_at"] != deadline.Format(time.RFC3339)) {
				t.Error("changed grant intent")
			}
			if !grant && len(body) != 0 {
				t.Error("revoke carries grant payload")
			}
			if calls == 1 {
				if r.URL.RawQuery != "dry_run=true" || r.Header.Get("If-Match") != "" || r.Header.Get("Idempotency-Key") != "" {
					t.Error("preview mutating headers/query")
				}
				if err := json.NewEncoder(w).Encode(preview); err != nil {
					t.Error(err)
				}
				return
			}
			if r.URL.RawQuery != "" || r.Header.Get("If-Match") != `"exact-revision"` || r.Header.Get("Idempotency-Key") != "stable-reviewed-key" {
				t.Error("apply refreshed consent")
			}
			if err := json.NewEncoder(w).Encode(accessapp.PermissionMutation{PermissionState: accessapp.PermissionState{Principal: principal, Workspace: "research", Revision: "revised", Permission: preview.After}}); err != nil {
				t.Error(err)
			}
		}))
		client := New(server.Client(), server.URL) // even legacy-root client uses only versioned permission paths.
		requestedExpiry := time.Time{}
		if grant {
			requestedExpiry = deadline
		}
		value, err := client.PreviewWorkspacePermission(t.Context(), "research", principal, grant, requestedExpiry)
		if err != nil {
			t.Fatal(err)
		}
		reviewed, err := ReviewPermission(value, "stable-reviewed-key")
		if err != nil {
			t.Fatal(err)
		}
		// The caller's mutable preview cannot alter the retained confirmed intent.
		value.Revision, value.Workspace = "new-revision", "other"
		if value.After != nil {
			*value.After.ExpiresAt = deadline.Add(time.Hour)
		}
		for range 2 {
			if _, err := client.ApplyWorkspacePermission(t.Context(), reviewed); err != nil {
				t.Fatal(err)
			}
		}
		if calls != 3 {
			t.Fatalf("calls=%d; unexpected discovery or automatic retry", calls)
		}
		server.Close()
	}
}

func TestPermissionClientRejectsInvalidReviewBeforeHTTP(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
	defer server.Close()
	client := New(server.Client(), server.URL)
	if _, err := client.ApplyWorkspacePermission(t.Context(), ReviewedPermission{}); err == nil {
		t.Fatal("zero intent applied")
	}
	principal := accessidentity.NewManaged([16]byte{1}).String()
	preview := accessapp.PermissionPreview{Principal: principal, Workspace: "research", Operation: "revoke", Revision: "reviewed", ApprovalClass: "trusted_confirmation", Warning: "confirm"}
	for _, key := range []string{"", "has space", "has\ttab", strings.Repeat("k", 257)} {
		if _, err := ReviewPermission(preview, key); err == nil {
			t.Fatal("invalid key admitted to reviewed intent")
		}
	}
	if _, err := ReviewPermission(preview, strings.Repeat("k", 256)); err != nil {
		t.Fatal("valid boundary key denied")
	}
	if _, err := client.PreviewWorkspacePermission(t.Context(), "research", principal, false, time.Now()); err == nil {
		t.Fatal("revoke deadline accepted")
	}
	if _, err := client.PreviewWorkspacePermission(t.Context(), "../other", principal, true, time.Time{}); err == nil {
		t.Fatal("invalid Workspace accepted")
	}
	if calls != 0 {
		t.Fatal("invalid intent contacted daemon")
	}
}

func TestPermissionClientRejectsPreviewScopeAndExpirySubstitution(t *testing.T) {
	principal := accessidentity.NewManaged([16]byte{1}).String()
	deadline := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, mismatch := range []string{"workspace", "principal", "operation", "expiry", "prior-action"} {
		t.Run(mismatch, func(t *testing.T) {
			value := deadline
			preview := accessapp.PermissionPreview{Principal: principal, Workspace: "research", Operation: "grant", Revision: "reviewed", ApprovalClass: "trusted_confirmation", Warning: "confirm", After: &accessapp.Permission{Workspace: "research", Action: "use", ExpiresAt: &value}}
			switch mismatch {
			case "workspace":
				preview.Workspace = "other"
			case "principal":
				preview.Principal = accessidentity.NewManaged([16]byte{2}).String()
			case "operation":
				preview.Operation = "revoke"
				preview.After = nil
			case "expiry":
				value = deadline.Add(time.Hour)
			case "prior-action":
				preview.Before = &accessapp.Permission{Workspace: "research", Action: "manage"}
			}
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if err := json.NewEncoder(w).Encode(preview); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			client := New(server.Client(), server.URL)
			if _, err := client.PreviewWorkspacePermission(t.Context(), "research", principal, true, deadline); err == nil {
				t.Fatal("substituted preview accepted")
			}
			if calls != 1 {
				t.Fatal("mismatched preview caused another request")
			}
		})
	}
}

func TestPermissionClientReadRejectsContradictoryOrSubstitutedState(t *testing.T) {
	principal := accessidentity.NewManaged([16]byte{1}).String()
	for _, invalid := range []string{"scope", "revision", "absent-allowed", "action", "zero-expiry"} {
		t.Run(invalid, func(t *testing.T) {
			state := accessapp.PermissionState{Principal: principal, Workspace: "research", Revision: "read-revision"}
			switch invalid {
			case "scope":
				state.Workspace = "other"
			case "revision":
				state.Revision = ""
			case "absent-allowed":
				state.Allowed = true
			case "action":
				state.Permission = &accessapp.Permission{Workspace: "research", Action: "manage"}
			case "zero-expiry":
				zero := time.Time{}
				state.Permission = &accessapp.Permission{Workspace: "research", Action: "use", ExpiresAt: &zero}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.RawQuery != "" || r.Header.Get("If-Match") != "" || r.Header.Get("Idempotency-Key") != "" {
					t.Error("read sent mutation intent")
				}
				if err := json.NewEncoder(w).Encode(state); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			if _, err := New(server.Client(), server.URL).GetWorkspacePermission(t.Context(), "research", principal); err == nil {
				t.Fatal("invalid relationship read accepted")
			}
		})
	}
}
