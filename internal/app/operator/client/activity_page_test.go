package operatorclient

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestActivityPageRejectsInvalidAcknowledgementWithoutRetry(t *testing.T) {
	for _, body := range []string{
		`{"scope":{"kind":"all"},"exchanges":[]}`,
		`{"scope":{"kind":"all"},"exchanges":[],"page":{"limit":1,"mode":"live"}}`,
		`{"scope":{"kind":"all"},"exchanges":[],"page":{"limit":2,"mode":"retained"}}`,
		`{"scope":{"kind":"all"},"exchanges":[{"exchange_id":"live","state":"in_progress"}],"page":{"limit":1,"mode":"retained"}}`,
		`{"scope":{"kind":"all"},"exchanges":[],"page":{"limit":1,"mode":"retained","exchange_next":"invalid"}}`,
		`{"scope":{"kind":"all"},"exchanges":[{},{}],"page":{"limit":1,"mode":"retained"}}`,
	} {
		var calls atomic.Int32
		node := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			calls.Add(1)
			if request.URL.RawQuery != "limit=1" {
				t.Error("wrong retained query")
			}
			fmt.Fprint(writer, body)
		}))
		client := New(node.Client(), node.URL)
		if _, err := client.ActivityPage(t.Context(), "", ActivityPageRequest{Limit: 1}); err == nil || calls.Load() != 1 {
			t.Fatal("invalid page accepted or retried")
		}
		node.Close()
	}
}
