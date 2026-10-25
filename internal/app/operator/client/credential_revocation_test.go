package operatorclient

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCredentialRevocationClientOneSendAndStrictAcknowledgement(t *testing.T) {
	const principal = "acc_AAAAAAAAAAAAAAAAAAAAAA"
	const credential = "acred_AAAAAAAAAAAAAAAAAAAAAA"
	for _, mode := range []string{"success", "replay", "lost", "missing-replay", "missing-revoked", "false-revoked", "wrong-credential", "wrong-principal", "wrong-etag", "null-permissions", "private-fields", "trailing"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			client := New(&http.Client{Transport: principalCreationTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodDelete || req.URL.Path != "/api/v1/principals/"+principal+"/credentials/"+credential || req.URL.RawQuery != "" || req.Header.Get("If-Match") != `"original"` || req.Header.Get("Idempotency-Key") != "retained" || req.GetBody != nil {
					t.Error("intent changed or replay enabled")
				}
				if mode == "lost" {
					return nil, errors.New("fixture-secret")
				}
				body := `{"principal":{"id":"` + principal + `","kind":"managed","status":"active","revision":"current","permissions":[]},"credential":"` + credential + `","revoked":true,"replayed":false}`
				switch mode {
				case "replay":
					body = strings.Replace(body, `"replayed":false`, `"replayed":true`, 1)
				case "missing-replay":
					body = strings.Replace(body, `,"replayed":false`, "", 1)
				case "missing-revoked":
					body = strings.Replace(body, `,"revoked":true`, "", 1)
				case "false-revoked":
					body = strings.Replace(body, `"revoked":true`, `"revoked":false`, 1)
				case "wrong-credential":
					body = strings.Replace(body, credential, "acred_BBBBBBBBBBBBBBBBBBBBBB", 1)
				case "wrong-principal":
					body = strings.Replace(body, principal, "acc_BBBBBBBBBBBBBBBBBBBBBB", 1)
				case "null-permissions":
					body = strings.Replace(body, `"permissions":[]`, `"permissions":null`, 1)
				case "private-fields":
					body = strings.Replace(body, `"permissions":[]`, `"permissions":[],"credential_revocation_receipts":{}`, 1)
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
			intent, err := RestoreCredentialRevocationIntent(principal, credential, "original", "retained")
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.ApplyCredentialRevocation(t.Context(), intent)
			if calls != 1 {
				t.Fatal("automatic retry")
			}
			if mode == "success" || mode == "replay" {
				if err != nil || !result.Revoked || result.Replayed != (mode == "replay") {
					t.Fatal("valid acknowledgement rejected", err)
				}
			} else if err == nil || result.Credential != "" || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("invalid acknowledgement exposed partial state or secret")
			}
		})
	}
}

func TestCredentialRevocationClientPreviewAndInvalidIntent(t *testing.T) {
	const principal = "acc_AAAAAAAAAAAAAAAAAAAAAA"
	const credential = "acred_AAAAAAAAAAAAAAAAAAAAAA"
	for _, mode := range []string{"success", "secret", "wrong-audience", "missing-time", "wrong-revision"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			client := New(&http.Client{Transport: principalCreationTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodDelete || req.URL.RawQuery != "dry_run=true" || req.Header.Get("If-Match") != "" || req.Header.Get("Idempotency-Key") != "" || req.GetBody != nil {
					t.Error("preview reserved consent/key or replay")
				}
				body := `{"principal":"` + principal + `","credential":{"id":"` + credential + `","workspace":"prod","created_at":"2026-10-11T00:00:00Z"},"revision":"reviewed","approval_class":"trusted_confirmation","warning":"Preview is not approval"}`
				switch mode {
				case "secret":
					body = strings.Replace(body, `"workspace":"prod"`, `"workspace":"prod","token":"fixture-secret"`, 1)
				case "wrong-audience":
					body = strings.Replace(body, `"workspace":"prod"`, `"workspace":"../bad"`, 1)
				case "missing-time":
					body = strings.Replace(body, `,"created_at":"2026-10-11T00:00:00Z"`, "", 1)
				case "wrong-revision":
					body = strings.Replace(body, `"revision":"reviewed"`, `"revision":"other"`, 1)
				}
				header := make(http.Header)
				header.Set("ETag", `"reviewed"`)
				return &http.Response{StatusCode: 200, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}, "http://127.0.0.1:1234")
			impact, err := client.PreviewCredentialRevocation(t.Context(), principal, credential)
			if calls != 1 {
				t.Fatal("preview retry")
			}
			if mode == "success" {
				if err != nil || impact.Revision != "reviewed" {
					t.Fatal(err)
				}
			} else if err == nil || impact.Revision != "" {
				t.Fatal("invalid preview accepted")
			}
			if _, err := client.ApplyCredentialRevocation(t.Context(), CredentialRevocationIntent{}); err == nil || calls != 1 {
				t.Fatal("zero intent reached transport")
			}
		})
	}
	for _, value := range []struct{ principal, credential, revision, key string }{
		{"bad", credential, "original", "retained"}, {principal, "bad", "original", "retained"}, {principal, credential, "", "retained"}, {principal, credential, `unsafe"revision`, "retained"}, {principal, credential, "original", "two keys"},
	} {
		if _, err := RestoreCredentialRevocationIntent(value.principal, value.credential, value.revision, value.key); err == nil {
			t.Fatal("invalid retained intent accepted")
		}
	}
}
