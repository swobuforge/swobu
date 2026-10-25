package operatorclient

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListWorkspaceToolsCanonicalReadAndExactScope(t *testing.T) {
	for _, available := range []bool{true, false} {
		t.Run(fmt.Sprint(available), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				if request.Method != http.MethodGet || request.URL.Path != "/api/v1/workspaces/research/tools" || request.URL.RawQuery != "" {
					t.Error("tools query escaped canonical read")
				}
				if !available {
					writer.WriteHeader(http.StatusNotFound)
					return
				}
				fmt.Fprint(writer, `[]`)
			}))
			defer server.Close()
			client := New(server.Client(), server.URL)
			tools, err := client.ListWorkspaceTools(t.Context(), "research")
			if calls != 1 || (!available && err == nil) || (available && (err != nil || tools == nil || len(tools) != 0)) {
				t.Fatal("tools read retried/fell back or lost empty catalog")
			}
			for _, workspace := range []string{"", "Research", "../research", "research "} {
				if _, err := client.ListWorkspaceTools(t.Context(), workspace); err == nil || calls != 1 {
					t.Fatal("invalid Workspace reached Node")
				}
			}
		})
	}
}

func TestListConnectionMCPToolsCanonicalRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/api/v1/connections/cn_mcp/mcp-catalog" || request.URL.RawQuery != "" {
			t.Errorf("unexpected request %s %s", request.Method, request.URL.RequestURI())
		}
		fmt.Fprint(writer, `[{"name":"search","remote_name":"search","connection_id":"cn_mcp","connection_revision":"4"}]`)
	}))
	defer server.Close()
	tools, err := New(server.Client(), server.URL).ListConnectionMCPTools(t.Context(), "cn_mcp")
	if err != nil || len(tools) != 1 || tools[0].ConnectionRevision != "4" {
		t.Fatalf("tools=%#v err=%v", tools, err)
	}
}
