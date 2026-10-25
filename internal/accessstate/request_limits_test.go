package accessstate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/swobuforge/swobu/internal/routing"
)

func rateTestWorkspace(t *testing.T) routing.WorkspaceSlug {
	t.Helper()
	ws, err := routing.ParseWorkspaceSlug("engineering")
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestPrincipalServiceRateEnforcedAcrossCredentialsAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "principals.json")
	store, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ws := rateTestWorkspace(t)
	principal, first, err := store.Create("build", ws)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.IssueCredential(principal.ID, ws)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 18, 17, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	rev, err := store.PermissionRevision(principal.ID, ws)
	if err != nil {
		t.Fatal(err)
	}
	services := []routing.ServiceKind{routing.ServiceModels, routing.ServiceMCP}
	rates := []RequestRateLimit{{Service: routing.ServiceModels, RequestsPerMinute: 2}, {Service: routing.ServiceMCP, RequestsPerMinute: 1}}
	preview, err := store.PreviewPermissionServicesWithRates(principal.ID, ws, rev, true, time.Time{}, services, rates)
	if err != nil || preview.After == nil || len(preview.After.RequestLimits) != 2 {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	result, err := store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, rev, "set-rates", func() error { return nil }, services, rates)
	if err != nil || result.Replayed {
		t.Fatal("rate policy apply", err)
	}
	for _, id := range []CredentialID{first.Credential.ID, second.Credential.ID} {
		if _, err := store.AdmitCredentialOperation(id, ws, routing.ServiceModels); err != nil {
			t.Fatal(err)
		}
	}
	if retry, err := store.AdmitCredentialOperation(first.Credential.ID, ws, routing.ServiceModels); !errors.Is(err, ErrRequestRateLimited) || retry < 29*time.Second || retry > 31*time.Second {
		t.Fatalf("Models limit=%s error=%v", retry, err)
	}
	if _, err := store.AdmitCredentialOperation(first.Credential.ID, ws, routing.ServiceMCP); err != nil {
		t.Fatal("MCP independent: ", err)
	}
	if _, err := store.AdmitCredentialOperation(second.Credential.ID, ws, routing.ServiceMCP); !errors.Is(err, ErrRequestRateLimited) {
		t.Fatalf("MCP limit: %v", err)
	}
	if _, err := store.AdmitCredentialOperation(first.Credential.ID, ws, routing.ServiceSearch); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Search scope widened: %v", err)
	}
	now = now.Add(30 * time.Second)
	if _, err := store.AdmitCredentialOperation(second.Credential.ID, ws, routing.ServiceModels); err != nil {
		t.Fatalf("Models did not replenish: %v", err)
	}
	reopened, err := openTestStore(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reopened.ReadPermission(principal.ID, ws)
	if err != nil || snapshot.Permission == nil || len(snapshot.Permission.RequestLimits) != 2 {
		t.Fatal("lost policy after restart", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"schema_version": 3`) || !strings.Contains(string(raw), `"request_limits"`) {
		t.Fatalf("new policy is not schema-versioned: %s", raw)
	}
}

func TestRateGrantRevisionReviewAndIdempotency(t *testing.T) {
	store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	ws := rateTestWorkspace(t)
	principal, first, err := store.Create("agent", ws)
	if err != nil {
		t.Fatal(err)
	}
	rev, _ := store.PermissionRevision(principal.ID, ws)
	scopes := []routing.ServiceKind{routing.ServiceModels}
	rates := []RequestRateLimit{{Service: routing.ServiceModels, RequestsPerMinute: 4}}
	if _, err := store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, rev, "change", func() error { return nil }, scopes, rates); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, rev, "change", func() error { return nil }, scopes, []RequestRateLimit{{Service: routing.ServiceModels, RequestsPerMinute: 3}}); !errors.Is(err, ErrPermissionIdempotencyConflict) {
		t.Fatalf("reused key changed policy: %v", err)
	}
	replay, err := store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, rev, "change", func() error { return nil }, scopes, rates)
	if err != nil || !replay.Replayed {
		t.Fatalf("exact replay: %+v %v", replay, err)
	}
	if _, err := store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, rev, "new", func() error { return nil }, scopes, rates); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update: %v", err)
	}
	newRev, _ := store.PermissionRevision(principal.ID, ws)
	// The older API cannot strip a rate ceiling by silently omitting rates.
	if _, err := store.ApplyPermissionServicesChecked(t.Context(), principal.ID, ws, newRev, "legacy", true, func() error { return nil }, scopes); err != nil {
		t.Fatal(err)
	}
	snap, _ := store.ReadPermission(principal.ID, ws)
	if len(snap.Permission.RequestLimits) != 1 || len(snap.Permission.Services) != 1 || snap.Permission.Services[0] != routing.ServiceModels {
		t.Fatal("legacy edit widened scoped access or disabled admission ceiling")
	}
	// An omitted Services field from an older client is not permission to
	// expand a scoped grant. A new grant may still use legacy Workspace-wide
	// access, but changing a scoped grant must preserve its scope.
	if _, err := store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, snap.Revision, "invalid-scope", func() error { return nil }, nil,
		[]RequestRateLimit{{Service: routing.ServiceMCP, RequestsPerMinute: 1}}); err == nil {
		t.Fatal("rate policy escaped the existing scoped grant")
	}
	// An explicit empty list clears the ceiling and does not revoke access.
	if _, err := store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, snap.Revision, "clear", func() error { return nil }, scopes, []RequestRateLimit{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := store.AdmitCredentialOperation(first.Credential.ID, ws, routing.ServiceModels); err != nil {
			t.Fatal("clear left rate ceiling", err)
		}
	}
}

func TestRequestRateValidationAndRevocationRace(t *testing.T) {
	store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	ws := rateTestWorkspace(t)
	principal, issued, err := store.Create("runner", ws)
	if err != nil {
		t.Fatal(err)
	}
	rev, _ := store.PermissionRevision(principal.ID, ws)
	scopes := []routing.ServiceKind{routing.ServiceModels}
	for _, invalid := range [][]RequestRateLimit{
		{{Service: routing.ServiceModels, RequestsPerMinute: 0}},
		{{Service: routing.ServiceModels, RequestsPerMinute: 100001}},
		{{Service: routing.ServiceMCP, RequestsPerMinute: 1}},
		{{Service: routing.ServiceModels, RequestsPerMinute: 1}, {Service: routing.ServiceModels, RequestsPerMinute: 2}},
	} {
		if _, err := store.PreviewPermissionServicesWithRates(principal.ID, ws, rev, true, time.Time{}, scopes, invalid); err == nil {
			t.Fatalf("accepted invalid rate policy: %+v", invalid)
		}
	}
	limits := []RequestRateLimit{{Service: routing.ServiceModels, RequestsPerMinute: 1}}
	if _, err := store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, rev, "limit", func() error { return nil }, scopes, limits); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	admitted := make(chan bool, 128)
	for i := 0; i < 128; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.AdmitCredentialOperation(issued.Credential.ID, ws, routing.ServiceModels)
			admitted <- err == nil
		}()
	}
	wg.Wait()
	close(admitted)
	count := 0
	for ok := range admitted {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("concurrent metering admitted %d requests; want 1", count)
	}
	rev, _ = store.PermissionRevision(principal.ID, ws)
	if _, err := store.ApplyPermissionChecked(t.Context(), principal.ID, ws, rev, "revoke", false, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitCredentialOperation(issued.Credential.ID, ws, routing.ServiceModels); !errors.Is(err, ErrUnauthorized) {
		t.Fatal("revoked policy still admits", err)
	}
}

func TestPrincipalRatePolicySurvivesInlineDocumentAndIsImmutableOnFailedWrite(t *testing.T) {
	var durable []byte
	publish := func(raw []byte) error {
		durable = append([]byte(nil), raw...)
		return nil
	}
	store, err := OpenInline(nil, publish)
	if err != nil {
		t.Fatal(err)
	}
	ws := rateTestWorkspace(t)
	principal, issued, err := store.Create("worker", ws)
	if err != nil {
		t.Fatal(err)
	}
	rev, _ := store.PermissionRevision(principal.ID, ws)
	limit := []RequestRateLimit{{Service: routing.ServiceModels, RequestsPerMinute: 1}}
	_, err = store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, rev, "one", func() error { return nil }, []routing.ServiceKind{routing.ServiceModels}, limit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateDocument(durable); err != nil {
		t.Fatal("inline Node document invalid", err)
	}
	loaded, err := OpenInline(durable, func([]byte) error { return errors.New("disk write failed") })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.AdmitCredentialOperation(issued.Credential.ID, ws, routing.ServiceModels); err != nil {
		t.Fatal(err)
	}
	if _, err := loaded.AdmitCredentialOperation(issued.Credential.ID, ws, routing.ServiceModels); !errors.Is(err, ErrRequestRateLimited) {
		t.Fatalf("inline rate bypassed: %v", err)
	}
	rev, _ = loaded.PermissionRevision(principal.ID, ws)
	_, err = loaded.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, rev, "clear", func() error { return nil }, []routing.ServiceKind{routing.ServiceModels}, []RequestRateLimit{})
	if err == nil {
		t.Fatal("failed inline write was reported as committed")
	}
	current, err := loaded.ReadPermission(principal.ID, ws)
	if err != nil || len(current.Permission.RequestLimits) != 1 || current.Revision != rev {
		t.Fatal("failed inline write changed live permission", err)
	}
}

func TestRateBucketsFollowCommittedPolicyLifecycle(t *testing.T) {
	store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
	if err != nil {
		t.Fatal(err)
	}
	ws := rateTestWorkspace(t)
	principal, issued, err := store.Create("runner", ws)
	if err != nil {
		t.Fatal(err)
	}
	rev, _ := store.PermissionRevision(principal.ID, ws)
	services := []routing.ServiceKind{routing.ServiceModels}
	rates := []RequestRateLimit{{Service: routing.ServiceModels, RequestsPerMinute: 1}}
	if _, err := store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, rev, "set", func() error { return nil }, services, rates); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitCredentialOperation(issued.Credential.ID, ws, routing.ServiceModels); err != nil {
		t.Fatal(err)
	}
	rev, _ = store.PermissionRevision(principal.ID, ws)
	// A successful unrelated metadata write cannot refill a live bucket.
	if _, err := store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, rev, "same", func() error { return nil }, services, rates); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitCredentialOperation(issued.Credential.ID, ws, routing.ServiceModels); !errors.Is(err, ErrRequestRateLimited) {
		t.Fatalf("publication reset live quota: %v", err)
	}
	rev, _ = store.PermissionRevision(principal.ID, ws)
	if _, err := store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, rev, "clear", func() error { return nil }, services, []RequestRateLimit{}); err != nil {
		t.Fatal(err)
	}
	if len(store.rateBuckets) != 0 {
		t.Fatal("cleared policy retained a bucket tombstone")
	}
}

func TestRatePolicyProjectionsCannotMutateLiveAuthority(t *testing.T) {
	for _, projection := range []string{"read", "preview-before", "preview-after", "apply", "audit"} {
		t.Run(projection, func(t *testing.T) {
			store, err := openTestStore(filepath.Join(t.TempDir(), "principals.json"))
			if err != nil {
				t.Fatal(err)
			}
			ws := rateTestWorkspace(t)
			principal, issued, err := store.Create("runner", ws)
			if err != nil {
				t.Fatal(err)
			}
			rev, _ := store.PermissionRevision(principal.ID, ws)
			result, err := store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, rev, "one", func() error { return nil }, []routing.ServiceKind{routing.ServiceModels}, []RequestRateLimit{{Service: routing.ServiceModels, RequestsPerMinute: 1}})
			if err != nil {
				t.Fatal(err)
			}
			switch projection {
			case "read":
				snapshot, err := store.ReadPermission(principal.ID, ws)
				if err != nil {
					t.Fatal(err)
				}
				snapshot.Permission.RequestLimits[0].RequestsPerMinute = 100
			case "preview-before", "preview-after":
				impact, err := store.PreviewPermissionServicesWithRates(principal.ID, ws, "", true, time.Time{}, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				if projection == "preview-before" {
					impact.Before.RequestLimits[0].RequestsPerMinute = 100
				} else {
					impact.After.RequestLimits[0].RequestsPerMinute = 100
				}
			case "apply":
				result.Permission.RequestLimits[0].RequestsPerMinute = 100
			case "audit":
				(*store.PermissionAudits()[0].Audit.RequestLimits)[0].RequestsPerMinute = 100
			}
			current, _ := store.ReadPermission(principal.ID, ws)
			if (*store.PermissionAudits()[0].Audit.RequestLimits)[0].RequestsPerMinute != 1 {
				t.Fatal("projection mutated retained retry audit")
			}
			if current.Permission.RequestLimits[0].RequestsPerMinute != 1 {
				t.Fatal("projection mutated live policy without publication")
			}
			if _, err := store.AdmitCredentialOperation(issued.Credential.ID, ws, routing.ServiceModels); err != nil {
				t.Fatal(err)
			}
			if _, err := store.AdmitCredentialOperation(issued.Credential.ID, ws, routing.ServiceModels); !errors.Is(err, ErrRequestRateLimited) {
				t.Fatalf("projection bypassed ceiling: %v", err)
			}
			if _, err := store.ApplyPermissionServicesWithRatesChecked(t.Context(), principal.ID, ws, rev, "one", func() error { return nil }, []routing.ServiceKind{routing.ServiceModels}, []RequestRateLimit{{Service: routing.ServiceModels, RequestsPerMinute: 1}}); err != nil {
				t.Fatalf("projection corrupted retry audit: %v", err)
			}
		})
	}
}
