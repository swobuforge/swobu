package operatorclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrowseCredentialFilesPreservesOpaqueReturnedPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("path"); got != `/home/operator/key` {
			t.Fatalf("query path = %q", got)
		}
		_, _ = w.Write([]byte(`{"path":"C:\\Users\\operator","parent":"C:\\Users","entries":[{"name":"key","path":"C:\\Users\\operator\\key","is_dir":false}]}`))
	}))
	defer server.Close()
	listing, err := New(server.Client(), server.URL).BrowseCredentialFiles(context.Background(), `/home/operator/key`)
	if err != nil {
		t.Fatal(err)
	}
	if listing.Path != `C:\Users\operator` || listing.Entries[0].Path != `C:\Users\operator\key` {
		t.Fatalf("listing normalized opaque paths: %#v", listing)
	}
}
