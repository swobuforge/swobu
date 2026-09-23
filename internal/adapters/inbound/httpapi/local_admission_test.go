package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/accessstate"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

func admittedRequest() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:9876/c/prod/responses", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	return r
}

func TestAuthorityFromRequestRequiresAdmissionContext(t *testing.T) {
	workspace, _ := routing.ParseWorkspaceSlug("prod")
	if id, err := authorityFromRequest(admittedRequest(), workspace); err == nil || id.Valid() {
		t.Fatalf("authority = %v, %v; missing admission context must fail closed", id, err)
	}
}

func TestManagedAccessCredentialQueryConsumptionPreservesUnrelatedValuesAndErasesRequestURI(t *testing.T) {
	store, _ := accessstate.Open(filepath.Join(t.TempDir(), "access.json"))
	workspace, _ := routing.ParseWorkspaceSlug("prod")
	_, issued, err := store.Create("CI", workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, rawQuery := range []string{
		"key=" + url.QueryEscape(issued.Token) + "&key=provider-key",
		"key=provider-key&key=" + url.QueryEscape(issued.Token),
		"key=" + url.QueryEscape(" "+issued.Token+" ") + "&key=provider-key",
	} {
		t.Run(rawQuery, func(t *testing.T) {
			r := admittedRequest()
			r.URL.RawQuery = rawQuery
			r.RequestURI = r.URL.RequestURI()
			if _, status, err := resolveRequestAuthority(r, store); err != nil || status != 0 {
				t.Fatalf("resolve = %d, %v", status, err)
			}
			if got := r.URL.Query()["key"]; len(got) != 1 || got[0] != "provider-key" {
				t.Fatalf("preserved key values = %#v", got)
			}
			if strings.Contains(r.URL.RawQuery, issued.Token) || strings.Contains(r.RequestURI, issued.Token) {
				t.Fatalf("secret survived: RawQuery=%q RequestURI=%q", r.URL.RawQuery, r.RequestURI)
			}
		})
	}
}

func TestManagedAccessAuthorizationUsesCaseInsensitiveBearerSchemeOnly(t *testing.T) {
	store, _ := accessstate.Open(filepath.Join(t.TempDir(), "access.json"))
	workspace, _ := routing.ParseWorkspaceSlug("prod")
	_, issued, _ := store.Create("CI", workspace)

	accepted := admittedRequest()
	accepted.Header.Set("Authorization", "bEaReR "+issued.Token)
	if _, status, err := resolveRequestAuthority(accepted, store); err != nil || status != 0 {
		t.Fatalf("case-insensitive Bearer rejected: %d, %v", status, err)
	}
	if accepted.Header.Get("Authorization") != "" {
		t.Fatal("recognized Authorization credential was not consumed")
	}

	for _, raw := range []string{issued.Token, "Bearer", "Basic " + issued.Token, "Bearer " + issued.Token + " extra"} {
		r := admittedRequest()
		r.Header.Set("Authorization", raw)
		authority, status, err := resolveRequestAuthority(r, store)
		if err != nil || status != 0 {
			t.Fatalf("non-credential Authorization %q affected local admission: %d, %v", raw, status, err)
		}
		if id, err := authority(workspace); err != nil || id != accessidentity.Local {
			t.Fatalf("authority for %q = %v, %v", raw, id, err)
		}
		if got := r.Header.Get("Authorization"); got != raw {
			t.Fatalf("unrecognized Authorization changed: got %q want %q", got, raw)
		}
	}
}

func TestValidateLocalAdmission(t *testing.T) {
	for name, mutate := range map[string]func(*http.Request){"native": func(*http.Request) {}, "matching origin": func(r *http.Request) { r.Header.Set("Origin", "http://127.0.0.1:9876") }} {
		t.Run(name, func(t *testing.T) {
			r := admittedRequest()
			mutate(r)
			if err := ValidateLocalAdmission(r); err != nil {
				t.Fatal(err)
			}
		})
	}
	for name, mutate := range map[string]func(*http.Request){"foreign origin": func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }, "null origin": func(r *http.Request) { r.Header.Set("Origin", "null") }, "hostname": func(r *http.Request) { r.Host = "evil.example" }, "foreign peer": func(r *http.Request) { r.RemoteAddr = "192.0.2.1:1234" }} {
		t.Run(name, func(t *testing.T) {
			r := admittedRequest()
			mutate(r)
			if err := ValidateLocalAdmission(r); err == nil {
				t.Fatal("admission unexpectedly succeeded")
			}
		})
	}
}

func TestManagedAccessCredentialSlotsAndConsumption(t *testing.T) {
	store, _ := accessstate.Open(filepath.Join(t.TempDir(), "access.json"))
	workspace, _ := routing.ParseWorkspaceSlug("prod")
	access, issued, err := store.Create("CI", workspace)
	if err != nil {
		t.Fatal(err)
	}
	slots := map[string]func(*http.Request){"authorization": func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+issued.Token) }, "x-api-key": func(r *http.Request) { r.Header.Set("x-api-key", issued.Token) }, "google": func(r *http.Request) { r.Header.Set("X-Goog-Api-Key", issued.Token) }, "query": func(r *http.Request) {
		q := r.URL.Query()
		q.Set("key", issued.Token)
		q.Set("alt", "sse")
		r.URL.RawQuery = q.Encode()
	}}
	for name, set := range slots {
		t.Run(name, func(t *testing.T) {
			r := admittedRequest()
			set(r)
			authority, status, err := resolveRequestAuthority(r, store)
			if err != nil || status != 0 {
				t.Fatalf("resolve=%d,%v", status, err)
			}
			id, err := authority(workspace)
			if err != nil || id != access.ID {
				t.Fatalf("authority=%v,%v", id, err)
			}
			if r.Header.Get("Authorization") == issued.Token || r.Header.Get("x-api-key") == issued.Token || r.Header.Get("X-Goog-Api-Key") == issued.Token || r.URL.Query().Get("key") != "" {
				t.Fatal("credential was not consumed")
			}
			if name == "query" && r.URL.Query().Get("alt") != "sse" {
				t.Fatal("unrelated query lost")
			}
		})
	}
}

func TestManagedAccessRejectsInvalidAmbiguousAndWrongWorkspace(t *testing.T) {
	store, _ := accessstate.Open(filepath.Join(t.TempDir(), "access.json"))
	prod, _ := routing.ParseWorkspaceSlug("prod")
	staging, _ := routing.ParseWorkspaceSlug("staging")
	_, issued, _ := store.Create("CI", prod)
	r := admittedRequest()
	r.Header.Set("Authorization", "Bearer swak_"+url.QueryEscape("bad"))
	if _, status, _ := resolveRequestAuthority(r, store); status != http.StatusUnauthorized {
		t.Fatalf("invalid status=%d", status)
	}
	r = admittedRequest()
	r.Header.Set("Authorization", "Bearer "+issued.Token)
	r.Header.Set("x-api-key", "swak_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	if _, status, _ := resolveRequestAuthority(r, store); status != http.StatusUnauthorized {
		t.Fatalf("ambiguous status=%d", status)
	}
	r = admittedRequest()
	r.Header.Set("Authorization", "Bearer "+issued.Token)
	authority, _, _ := resolveRequestAuthority(r, store)
	if _, err := authority(staging); err == nil {
		t.Fatal("wrong Workspace authorized")
	}
	if id, err := authority(prod); err != nil || id == accessidentity.Local {
		t.Fatalf("managed authority=%v,%v", id, err)
	}
}
