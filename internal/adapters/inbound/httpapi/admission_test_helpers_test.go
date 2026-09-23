package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/swobuforge/swobu/internal/domain/accessidentity"
	"github.com/swobuforge/swobu/internal/routing"
)

// localAuthorityTestHandler models the authority supplied by trusted loopback
// admission without restoring a production fallback in protocol handlers.
func localAuthorityTestHandler(next http.Handler) http.Handler {
	return authorityTestHandler(next, accessidentity.Local)
}

func authorityTestHandler(next http.Handler, accessID accessidentity.ID) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authority := admittedAuthority(func(routing.WorkspaceSlug) (accessidentity.ID, error) {
			return accessID, nil
		})
		next.ServeHTTP(w, r.WithContext(contextWithAuthority(r, authority)))
	})
}

func operatorJSONTestRequest(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}
