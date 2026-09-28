package operatorclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientActivity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_swobu/activity" || r.URL.Query().Get("workspace") != "dev" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"state":"running","exchanges":[{"exchange_id":"ex-1","workspace":"dev"}]}`))
	}))
	defer server.Close()
	out, err := New(server.Client(), server.URL).Activity(context.Background(), "dev")
	if err != nil || len(out.Exchanges) != 1 || out.Exchanges[0].ExchangeID != "ex-1" {
		t.Fatalf("activity = %#v, err=%v", out, err)
	}
}
