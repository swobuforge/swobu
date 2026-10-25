package operatorclient

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInspectNodeCanonicalReadWithoutFallback(t *testing.T) {
	for _, available := range []bool{true, false} {
		t.Run(fmt.Sprint(available), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				if request.Method != http.MethodGet || request.URL.Path != "/api/v1/status" || request.URL.RawQuery != "" {
					t.Error("Node status escaped canonical read")
				}
				if !available {
					writer.WriteHeader(http.StatusNotFound)
					return
				}
				fmt.Fprint(writer, `{"state":"healthy","workspace_count":0,"control_plane_protocol":11,"swobu_version":"fixture-version","token":"private-extension"}`)
			}))
			defer server.Close()
			status, err := New(server.Client(), server.URL).InspectNode(t.Context())
			if calls != 1 || (!available && err == nil) || (available && (err != nil || status != (NodeStatus{State: "healthy", WorkspaceCount: 0, ControlPlaneProtocol: 11, SwobuVersion: "fixture-version"}))) {
				t.Fatal("Node status lost projection or retried/fell back")
			}
		})
	}
}
