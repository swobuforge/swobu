package operatorclient

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListLocalClientsCanonicalClosedMetadata(t *testing.T) {
	for _, body := range []string{`[]`, `[{"id":"codex","name":"Codex","token":"fixture-secret"}]`, `null`, `[{"id":"unknown","name":"Unknown"}]`, `[{"id":"codex","name":""}]`, `[{"id":"codex","name":"Codex"},{"id":"codex","name":"Codex"}]`} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				if request.Method != http.MethodGet || request.URL.Path != "/api/v1/clients" || request.URL.RawQuery != "workspace=research" {
					t.Error("discovery escaped canonical read")
				}
				fmt.Fprint(writer, body)
			}))
			defer server.Close()
			client := New(server.Client(), server.URL)
			clients, err := client.ListLocalClients(t.Context(), "research")
			valid := body == `[]` || body == `[{"id":"codex","name":"Codex","token":"fixture-secret"}]`
			if calls != 1 || (valid && (err != nil || clients == nil)) || (!valid && err == nil) {
				t.Fatal("discovery accepted malformed metadata or retried")
			}
			for _, workspace := range []string{"", "Research", "../research", "research "} {
				if _, err := client.ListLocalClients(t.Context(), workspace); err == nil || calls != 1 {
					t.Fatal("invalid Workspace reached Node")
				}
			}
		})
	}
}
