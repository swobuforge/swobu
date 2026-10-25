package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	accessapp "github.com/swobuforge/swobu/internal/app/operator/access"
	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

func TestCredentialIssuanceLostResponseDoesNotMintOrRevealAgain(t *testing.T) {
	h := accessHandler(t)
	created, err := h.service.Create(t.Context(), "retry-fixture", "prod")
	if err != nil {
		t.Fatal(err)
	}
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/principals/"+created.Access.ID+"/credentials", strings.NewReader(`{"workspace":"prod"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "lost-response-key")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, permissionOperatorRequest(r))
		return w
	}
	first := request() // The operator never receives this response.
	if first.Code != http.StatusCreated {
		t.Fatalf("first status=%d", first.Code)
	}
	var issued accessapp.IssueResult
	if json.Unmarshal(first.Body.Bytes(), &issued) != nil || issued.Token == "" {
		t.Fatal("first issuance omitted secret")
	}
	replay := request()
	var result map[string]json.RawMessage
	if json.Unmarshal(replay.Body.Bytes(), &result) != nil {
		t.Fatal("invalid replay response")
	}
	if replay.Code != http.StatusOK || string(result["secret_not_recoverable"]) != "true" || len(result["token"]) != 0 {
		t.Fatalf("replay status=%d; expected metadata-only unrecoverable outcome", replay.Code)
	}
	if len(h.service.List(t.Context())[1].Credentials) != 2 {
		t.Fatal("retry minted another credential")
	}
	var metadata struct {
		Credential accessapp.Credential `json:"credential"`
	}
	if json.Unmarshal(replay.Body.Bytes(), &metadata) != nil || metadata.Credential.ID != issued.Credential.ID {
		t.Fatal("replay lost original credential metadata")
	}
}

func TestCredentialIssuanceRejectsInvalidKeys(t *testing.T) {
	for name, keys := range map[string][]string{"missing": nil, "duplicate": {"a", "b"}, "space": {"not stable"}, "oversize": {strings.Repeat("k", 257)}} {
		t.Run(name, func(t *testing.T) {
			h := accessHandler(t)
			created, err := h.service.Create(t.Context(), "fixture", "prod")
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPost, "/api/v1/principals/"+created.Access.ID+"/credentials", strings.NewReader(`{"workspace":"prod"}`))
			for _, key := range keys {
				r.Header.Add("Idempotency-Key", key)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, permissionOperatorRequest(r))
			if w.Code != http.StatusBadRequest || len(h.service.List(t.Context())[1].Credentials) != 1 {
				t.Fatal("invalid key admitted issuance")
			}
		})
	}
}

func TestCredentialIssuanceRechecksAuthorityAtTransaction(t *testing.T) {
	for _, replay := range []bool{false, true} {
		h := accessHandler(t)
		created, err := h.service.Create(t.Context(), "fixture", "prod")
		if err != nil {
			t.Fatal(err)
		}
		request := func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/api/v1/principals/"+created.Access.ID+"/credentials", strings.NewReader(`{"workspace":"prod"}`))
			r.Header.Set("Idempotency-Key", "admission-key")
			r.Header.Set("Content-Type", "application/json")
			return r
		}
		if replay {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, permissionOperatorRequest(request()))
			if w.Code != 201 {
				t.Fatal("initial issuance failed")
			}
		}
		before := len(h.service.List(t.Context())[1].Credentials)
		checks := 0
		r := request()
		r = r.WithContext(contextWithAuthority(r, admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) {
			checks++
			if checks >= 2 {
				return accessidentity.ID{}, errors.New("authority withdrawn")
			}
			return accessidentity.Local, nil
		})))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 || checks != 2 || len(h.service.List(t.Context())[1].Credentials) != before {
			t.Fatalf("transaction denial status=%d checks=%d count=%d before=%d", w.Code, checks, len(h.service.List(t.Context())[1].Credentials), before)
		}
	}
}

func TestLegacyCredentialIssuanceRoutesAreRetired(t *testing.T) {
	h := accessHandler(t)
	created, err := h.service.Create(t.Context(), "fixture", "prod")
	if err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"/_swobu/access/", "/api/v1/access/"} {
		r := httptest.NewRequest(http.MethodPost, prefix+created.Access.ID+"/credentials", strings.NewReader(`{"workspace":"prod"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "retired-key")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, permissionOperatorRequest(r))
		if w.Code != 404 && w.Code != 405 {
			t.Fatalf("retired issuance status=%d", w.Code)
		}
	}
	if len(h.service.List(t.Context())[1].Credentials) != 1 {
		t.Fatal("retired route issued a credential")
	}
}
