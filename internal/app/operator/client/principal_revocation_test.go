package operatorclient

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPrincipalRevocationClientRejectsContradictoryAcknowledgementWithoutRetry(t *testing.T) {
	const id = "acc_AAAAAAAAAAAAAAAAAAAAAA"
	for _, mode := range []string{"success", "replay", "lost", "missing-replay", "missing-count", "active", "wrong-id", "wrong-etag", "secret", "null-permissions", "trailing"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			client := New(&http.Client{Transport: principalCreationTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/principals/"+id || r.URL.RawQuery != "" || r.Header.Get("If-Match") != `"original"` || r.Header.Get("Idempotency-Key") != "retained" || r.GetBody != nil {
					t.Error("intent changed or transport replay enabled")
				}
				if mode == "lost" {
					return nil, errors.New("fixture-secret")
				}
				body := `{"principal":{"id":"` + id + `","label":"Retained","kind":"managed","status":"revoked","revision":"current","permissions":[]},"credential_count":0,"replayed":false}`
				switch mode {
				case "replay":
					body = strings.Replace(body, "false", "true", 1)
				case "missing-replay":
					body = strings.Replace(body, `,"replayed":false`, "", 1)
				case "missing-count":
					body = strings.Replace(body, `,"credential_count":0`, "", 1)
				case "active":
					body = strings.Replace(body, "revoked", "active", 1)
				case "wrong-id":
					body = strings.Replace(body, id, "acc_BBBBBBBBBBBBBBBBBBBBBB", 1)
				case "secret":
					body = strings.Replace(body, `"permissions":[]`, `"permissions":[],"token":"fixture-secret"`, 1)
				case "null-permissions":
					body = strings.Replace(body, `"permissions":[]`, `"permissions":null`, 1)
				case "trailing":
					body += `{}`
				}
				header := make(http.Header)
				header.Set("ETag", `"current"`)
				if mode == "wrong-etag" {
					header.Set("ETag", `"wrong"`)
				}
				return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}, "http://127.0.0.1:1234")
			intent, err := RestorePrincipalRevocationIntent(id, "original", "retained")
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.ApplyPrincipalRevocation(t.Context(), intent)
			if calls != 1 {
				t.Fatal("automatic retry or refreshed consent")
			}
			if mode == "success" || mode == "replay" {
				if err != nil || result.Principal.ID != id {
					t.Fatal("valid acknowledgement rejected")
				}
			} else if err == nil || result.Principal.ID != "" || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("invalid acknowledgement exposed partial state or secret")
			}
		})
	}
}
