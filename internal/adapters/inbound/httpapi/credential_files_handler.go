package httpapi

import (
	"context"
	"net/http"

	"github.com/swobuforge/swobu/internal/app/operator/credentialfiles"
)

type CredentialFilesQuery func(context.Context, string) (credentialfiles.Listing, error)

func NewCredentialFilesHandler(browse CredentialFilesQuery) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if browse == nil {
			writeWorkspaceJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "UNAVAILABLE", "message": "credential files are unavailable"})
			return
		}
		listing, err := browse(r.Context(), r.URL.Query().Get("path"))
		if err != nil {
			writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{"code": "INVALID_ARGUMENT", "message": "could not browse credential directory"})
			return
		}
		writeWorkspaceJSON(w, http.StatusOK, listing)
	})
}
