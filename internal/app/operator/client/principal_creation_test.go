package operatorclient

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type principalCreationTransport func(*http.Request) (*http.Response, error)

func (f principalCreationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPrincipalCreationRetainsScopeAndDisablesBodyReplay(t *testing.T) {
	for _, outcome := range []string{"created", "replayed", "replay-permission", "replay-revoked", "lost", "wrong-label", "invalid-id", "fresh-permission", "fresh-revoked", "secret", "null-permissions", "missing-permissions", "trailing"} {
		calls := 0
		client := New(&http.Client{Transport: principalCreationTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			raw, _ := io.ReadAll(r.Body)
			if r.URL.Path != "/api/v1/principals" || r.Method != "POST" || r.Header.Get("Idempotency-Key") != "original-key" || r.GetBody != nil || string(raw) != `{"label":"Independent"}` {
				t.Error("creation altered scope or enabled automatic replay")
			}
			if outcome == "lost" {
				return nil, errors.New("disconnected")
			}
			label, id := "Independent", "acc_AAAAAAAAAAAAAAAAAAAAAA"
			if outcome == "wrong-label" {
				label = "Changed"
			}
			if outcome == "invalid-id" {
				id = "local"
			}
			status := 201
			if strings.HasPrefix(outcome, "replay") {
				status = 200
			}
			permissions, state, extra := "[]", "active", ""
			if outcome == "fresh-permission" || outcome == "replay-permission" {
				permissions = `[{"workspace":"research","action":"use"}]`
			}
			if outcome == "fresh-revoked" || outcome == "replay-revoked" {
				state = "revoked"
			}
			if outcome == "secret" {
				extra = `,"token":"fixture-secret"`
			}
			body := `{"id":"` + id + `","label":"` + label + `","kind":"managed","status":"` + state + `","permissions":` + permissions + extra + `}`
			if outcome == "null-permissions" {
				body = strings.Replace(body, `"permissions":[]`, `"permissions":null`, 1)
			}
			if outcome == "missing-permissions" {
				body = strings.Replace(body, `,"permissions":[]`, "", 1)
			}
			if outcome == "trailing" {
				body += `{}`
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}, "http://127.0.0.1:7926")
		_, replayed, err := client.CreatePrincipal(t.Context(), "Independent", "original-key")
		if calls != 1 {
			t.Fatal("creation retried")
		}
		if outcome == "created" || strings.HasPrefix(outcome, "replay") {
			if err != nil || replayed != strings.HasPrefix(outcome, "replay") {
				t.Fatal("creation acknowledgement incorrect")
			}
		} else if err == nil {
			t.Fatal("uncertain or contradictory acknowledgement accepted")
		}
	}
}
