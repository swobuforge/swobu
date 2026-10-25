package operatorclient

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCredentialAcknowledgementMustMatchIntentAndOneShotOutcome(t *testing.T) {
	const principal = "acc_AAAAAAAAAAAAAAAAAAAAAA"
	for _, invalid := range []string{
		`{"access":{"id":"wrong"},"credential":{"id":"fixture","workspace":"research"},"secret_not_recoverable":true}`,
		`{"access":{"id":"` + principal + `"},"credential":{"id":"fixture","workspace":"coding"},"secret_not_recoverable":true}`,
		`{"access":{"id":"` + principal + `"},"credential":{"id":"fixture","workspace":"research"},"token":"unexpected","secret_not_recoverable":true}`,
		`{"access":{"id":"` + principal + `"},"credential":{"id":"fixture","workspace":"research"}}`,
	} {
		writes := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { writes++; fmt.Fprint(w, invalid) }))
		intent, err := RestoreCredentialIssuanceIntent(principal, "research", "original-key")
		if err != nil {
			t.Fatal(err)
		}
		_, err = New(server.Client(), server.URL).ApplyCredentialIssuance(t.Context(), intent)
		server.Close()
		if err == nil || writes != 1 {
			t.Fatal("invalid acknowledgement accepted or retried")
		}
	}
}
