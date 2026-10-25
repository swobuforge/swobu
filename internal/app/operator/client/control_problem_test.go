package operatorclient

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestControlProblemPreservesCodeAndRecoveryMessage(t *testing.T) {
	for _, tc := range []struct {
		body string
		want string
	}{
		{`{"code":"PRECONDITION_REQUIRED","title":"Precondition Required","detail":"If-Match is required."}`, "If-Match is required."},
		{`{"code":"PRECONDITION_REQUIRED","title":"Precondition Required"}`, "Precondition Required"},
	} {
		response := &http.Response{
			StatusCode: 428,
			Header:     http.Header{"Content-Type": {"application/problem+json"}},
			Body:       io.NopCloser(strings.NewReader(tc.body)),
		}
		err := errorFromResponse(response, "mutation failed")
		var typed *ResponseError
		if !errors.As(err, &typed) || typed.Code != "PRECONDITION_REQUIRED" || typed.StatusCode != 428 || typed.Message != tc.want {
			t.Fatalf("error=%+v", err)
		}
	}
}
