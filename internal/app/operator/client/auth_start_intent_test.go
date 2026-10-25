package operatorclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/swobuforge/swobu/internal/app/operator/authplane"
)

func TestAuthStartIntentRecoveryRetainsOriginalRequestAndKey(t *testing.T) {
	input := authplane.StartInput{ProviderSpec: "chatgpt", DraftSubject: "original-draft", AuthMode: "device"}
	intent, err := NewAuthStartIntent(input, "original-start-key")
	if err != nil {
		t.Fatal(err)
	}
	input.DraftSubject = "later-draft"
	record, err := intent.RecoveryRecord()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreAuthStartIntent(record)
	if err != nil {
		t.Fatal(err)
	}
	review, key, err := restored.Review()
	if err != nil || review.DraftSubject != "original-draft" || key != "original-start-key" {
		t.Fatalf("review=%#v key=%s error=%v", review, key, err)
	}
	record[0] = '!'
	review, _, err = restored.Review()
	if err != nil || review.DraftSubject != "original-draft" {
		t.Fatal("record mutation changed frozen intent")
	}
	canonical, _ := restored.RecoveryRecord()
	for _, bad := range []string{string(canonical) + " ", strings.Replace(string(canonical), `"version":1`, `"version":1,"version":1`, 1), strings.Replace(string(canonical), `"version":1`, `"version":2`, 1), strings.Replace(string(canonical), `"request":`, `"approval":true,"request":`, 1), strings.Replace(string(canonical), `"auth_mode":"device"`, `"auth_mode":null`, 1)} {
		if _, err := RestoreAuthStartIntent([]byte(bad)); err == nil {
			t.Fatalf("invalid record accepted: %s", bad)
		}
	}
}

func TestApplyAuthStartIntentCanonicalOneSendAndReadback(t *testing.T) {
	for _, outcome := range []string{"created", "lost", "conflict", "provider", "id", "state", "url", "expiry", "code", "location", "trailing"} {
		t.Run(outcome, func(t *testing.T) {
			intent, err := NewAuthStartIntent(authplane.StartInput{ProviderSpec: "chatgpt", DraftSubject: "original-draft", AuthMode: "device"}, "original-start-key")
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			client := New(&http.Client{Transport: principalCreationTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				raw, _ := io.ReadAll(request.Body)
				if request.Method != "POST" || request.URL.Path != "/api/v1/auth-sessions" || request.GetBody != nil || request.Header.Get("Idempotency-Key") != "original-start-key" || !strings.Contains(string(raw), `"draft_subject":"original-draft"`) {
					t.Error("original handoff changed")
				}
				if outcome == "lost" {
					return nil, errors.New("response lost")
				}
				status := 201
				if outcome == "conflict" {
					status = 409
				}
				provider, id, state, address, expiry, code := "chatgpt", "sess-1", "pending", "https://example.test/login", "2026-10-14T00:00:00Z", "fixture-code"
				switch outcome {
				case "provider":
					provider = "other"
				case "id":
					id = "bad/id"
				case "state":
					state = "succeeded"
				case "url":
					address = "http://example.test/login"
				case "expiry":
					expiry = ""
				case "code":
					code = ""
				}
				body := `{"provider_spec":"` + provider + `","session_id":"` + id + `","state":"` + state + `","authorize_url":"` + address + `","expires_at":"` + expiry + `","user_code":"` + code + `"}`
				if outcome == "trailing" {
					body += `{}`
				}
				header := make(http.Header)
				header.Set("Location", "/api/v1/auth-sessions/"+id)
				if outcome == "location" {
					header.Set("Location", "/other")
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}, "http://127.0.0.1:7926")
			_, err = client.ApplyAuthStartIntent(context.Background(), intent)
			if calls != 1 || (err == nil) != (outcome == "created") {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}
