package operatorclient

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPrincipalLabelClientRetainsReviewedRevisionAndNeverRetries(t *testing.T) {
	const id = "acc_AAAAAAAAAAAAAAAAAAAAAA"
	for _, outcome := range []string{"updated", "noop", "revoked", "lost", "stale", "wrong-id", "wrong-label", "missing-revision", "wrong-etag", "secret", "null-permissions", "trailing"} {
		t.Run(outcome, func(t *testing.T) {
			calls := 0
			client := New(&http.Client{Transport: principalCreationTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				raw, _ := io.ReadAll(r.Body)
				if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/principals/"+id || r.URL.RawQuery != "" || r.Header.Get("If-Match") != `"reviewed"` || r.GetBody != nil || string(raw) != `{"label":"After"}` {
					t.Error("client changed reviewed intent or enabled replay")
				}
				if outcome == "lost" {
					return nil, errors.New("fixture-secret")
				}
				status := http.StatusOK
				if outcome == "stale" {
					status = http.StatusPreconditionFailed
				}
				identity, label, revision, state := id, "After", "committed", "active"
				if outcome == "noop" {
					revision = "reviewed"
				}
				if outcome == "revoked" {
					state = "revoked"
				}
				if outcome == "wrong-id" {
					identity = "acc_BBBBBBBBBBBBBBBBBBBBBB"
				}
				if outcome == "wrong-label" {
					label = "Different"
				}
				if outcome == "missing-revision" {
					revision = ""
				}
				body := `{"id":"` + identity + `","label":"` + label + `","revision":"` + revision + `","kind":"managed","status":"` + state + `","permissions":[]}`
				if outcome == "secret" {
					body = strings.TrimSuffix(body, "}") + `,"token":"fixture-secret"}`
				}
				if outcome == "null-permissions" {
					body = strings.Replace(body, `"permissions":[]`, `"permissions":null`, 1)
				}
				if outcome == "trailing" {
					body += `{}`
				}
				header := make(http.Header)
				header.Set("ETag", `"`+revision+`"`)
				if outcome == "wrong-etag" {
					header.Set("ETag", `"different"`)
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}, "http://127.0.0.1:1234")
			principal, err := client.UpdatePrincipalLabel(t.Context(), id, "After", "reviewed")
			if calls != 1 {
				t.Fatal("client retried or inspected new consent")
			}
			if outcome == "updated" || outcome == "noop" || outcome == "revoked" {
				if err != nil || principal.ID != id {
					t.Fatal("valid current metadata rejected")
				}
			} else if err == nil || principal.ID != "" || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("invalid response leaked partial metadata or secret")
			}
		})
	}
}
