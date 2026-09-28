package httpapi

import (
	"net/http/httptest"
	"net/url"
	"testing"

	store "github.com/swobuforge/swobu/internal/adapters/outbound/executionevidence"
)

func TestActivityScopeUsesWorkspaceSlugGrammar(t *testing.T) {
	for _, test := range []struct {
		workspace string
		wantKind  store.ProjectionScopeKind
		wantSlug  string
		valid     bool
	}{
		{"", store.ProjectionScopeAll, "", true},
		{"valid-workspace", store.ProjectionScopeWorkspace, "valid-workspace", true},
		{"INVALID WORKSPACE", "", "", false},
	} {
		request := httptest.NewRequest("GET", "/_swobu/activity?workspace="+url.QueryEscape(test.workspace), nil)
		scope, err := parseActivityScope(request)
		if (err == nil) != test.valid || (test.valid && (scope.Kind != test.wantKind || scope.Workspace != test.wantSlug)) {
			t.Fatalf("workspace=%q scope=%#v err=%v", test.workspace, scope, err)
		}
	}
}
