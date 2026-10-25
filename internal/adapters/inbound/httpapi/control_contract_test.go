package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStrongRevisionContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		status int
	}{
		{"missing", nil, 428}, {"empty", []string{""}, 428},
		{"weak", []string{`W/"rev"`}, 400}, {"wildcard", []string{"*"}, 400},
		{"list", []string{`"one", "two"`}, 400}, {"duplicate", []string{`"rev"`, `"rev"`}, 400},
		{"space", []string{`"two words"`}, 400}, {"unquoted", []string{"rev"}, 400},
		{"escaped", []string{`"rev\x"`}, 400}, {"empty-tag", []string{`""`}, 400},
		{"valid", []string{`"rev-01_ab.cd:ef~"`}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := http.Header{}
			for _, value := range tc.values {
				header.Add("If-Match", value)
			}
			revision, problem := StrongRevision(header)
			if tc.status == 0 {
				if problem != nil || revision != "rev-01_ab.cd:ef~" {
					t.Fatalf("revision=%q problem=%+v", revision, problem)
				}
			} else if problem == nil || problem.Status != tc.status || revision != "" {
				t.Fatalf("revision=%q problem=%+v, want %d", revision, problem, tc.status)
			}
		})
	}
}

func TestIdempotencyKeyContract(t *testing.T) {
	for _, values := range [][]string{nil, {""}, {"one", "two"}, {"has space"}, {"has\t tab"}, {"line\nfeed"}, {"é"}, {strings.Repeat("x", 257)}} {
		header := http.Header{"Idempotency-Key": values}
		if key, problem := IdempotencyKey(header); key != "" || problem == nil || problem.Status != 400 {
			t.Fatalf("accepted invalid key: %q", values)
		}
	}
	for _, value := range []string{"original-intent", strings.Repeat("x", 256)} {
		key, problem := IdempotencyKey(http.Header{"Idempotency-Key": {value}})
		if key != value || problem != nil {
			t.Fatalf("key=%q problem=%+v", key, problem)
		}
	}
}

func TestControlProblemRepresentation(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/workspaces?launch_token=private-marker", strings.NewReader("body-private-marker"))
	request.Header.Set("Authorization", "Bearer header-private-marker")
	response := httptest.NewRecorder()
	problem := contractProblem(412, "REVISION_CONFLICT", "revision-conflict", "Configuration changed.")
	problem.Recovery = &ProblemRecovery{Action: "reload"}
	WriteControlProblem(response, request, *problem)
	if response.Code != 412 || response.Header().Get("Content-Type") != "application/problem+json" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response=%v", response.Result())
	}
	var decoded ControlProblem
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Code != "REVISION_CONFLICT" || decoded.Instance != "/api/v1/workspaces" || decoded.Recovery.Action != "reload" || strings.Contains(response.Body.String(), "private-marker") {
		t.Fatalf("unexpected problem: %s", response.Body)
	}
}

func TestPermissionBoundaryUsesCanonicalProblemsWithoutMutation(t *testing.T) {
	handler := accessHandler(t)
	created, err := handler.service.Create(t.Context(), "fixture", "prod")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		headers http.Header
		status  int
		code    string
	}{
		{"missing revision", nil, 428, "PRECONDITION_REQUIRED"},
		{"weak revision", http.Header{"If-Match": {`W/"rev"`}}, 400, "INVALID_PRECONDITION"},
		{"missing retry key", http.Header{"If-Match": {`"reviewed"`}}, 400, "INVALID_IDEMPOTENCY_KEY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := handler.service.Revision()
			r := httptest.NewRequest(http.MethodDelete, "/api/v1/workspaces/prod/permissions/"+created.Access.ID, strings.NewReader(`{}`))
			if tc.headers != nil {
				r.Header = tc.headers
			}
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, permissionOperatorRequest(r))
			var problem ControlProblem
			if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if w.Code != tc.status || problem.Code != tc.code || w.Header().Get("Content-Type") != "application/problem+json" || handler.service.Revision() != before {
				t.Fatalf("status=%d problem=%+v", w.Code, problem)
			}
		})
	}
}
