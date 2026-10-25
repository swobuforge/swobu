package cli

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestConnectWithoutReviewedModeCannotContactNodeOrApply(t *testing.T) {
	for _, args := range [][]string{
		{"codex"}, {"codex", "--workspace", "research"},
		{"codex", "--workspace", "research", "--replace"},
	} {
		calls := 0
		var stdout, stderr strings.Builder
		code := runConnect(t.Context(), &http.Client{Transport: unexpectedRequestTransport{calls: &calls}}, &stdout, &stderr, args, Runner{IsInteractive: func() bool { return true }})
		if code != ExitDown || calls != 0 || stdout.Len() != 0 {
			t.Fatal("unchecked Connect contacted Node or emitted configured success")
		}
	}
}

// unexpectedRequestTransport proves validation fails before contacting the Node.
type unexpectedRequestTransport struct{ calls *int }

func (r unexpectedRequestTransport) RoundTrip(*http.Request) (*http.Response, error) {
	*r.calls++
	return nil, errors.New("unexpected Node request")
}
