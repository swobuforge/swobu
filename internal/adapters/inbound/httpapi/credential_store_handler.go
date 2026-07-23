package httpapi

import (
	"context"
	"net/http"
	"strings"
)

// CredentialStoreCommand persists transient credential material in the daemon
// process and returns the durable reference understood by runtime resolvers.
type CredentialStoreCommand func(context.Context, string, string, string) (string, error)

// NewCredentialStoreHandler keeps secret persistence in the daemon, whose
// state root and write policy also govern provider credential resolution.
func NewCredentialStoreHandler(store CredentialStoreCommand) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			ProviderSpec string `json:"provider_spec"`
			Name         string `json:"name"`
			Secret       string `json:"secret"`
		}
		if err := decodeOperatorJSONObject(w, r, &body, "credential store command"); err != nil {
			writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{
				"code": "INVALID_ARGUMENT", "message": "invalid credential store command: " + err.Error(),
			})
			return
		}
		body.ProviderSpec = strings.TrimSpace(body.ProviderSpec) // swobu:io-string source=boundary
		body.Name = strings.TrimSpace(body.Name)                 // swobu:io-string source=boundary
		body.Secret = strings.TrimSpace(body.Secret)             // swobu:io-string source=boundary
		if body.ProviderSpec == "" || body.Name == "" || body.Secret == "" {
			writeWorkspaceJSON(w, http.StatusBadRequest, map[string]string{
				"code": "INVALID_ARGUMENT", "message": "provider spec, credential slot, and secret are required",
			})
			return
		}
		if store == nil {
			writeWorkspaceJSON(w, http.StatusServiceUnavailable, map[string]string{
				"code": "UNAVAILABLE", "message": "credential store is unavailable",
			})
			return
		}
		ref, err := store(r.Context(), body.ProviderSpec, body.Name, body.Secret)
		if err != nil {
			writeWorkspaceJSON(w, http.StatusInternalServerError, map[string]string{
				"code": "INTERNAL", "message": "credential could not be stored",
			})
			return
		}
		ref = strings.TrimSpace(ref) // swobu:io-string source=boundary
		if ref == "" {
			writeWorkspaceJSON(w, http.StatusInternalServerError, map[string]string{
				"code": "INTERNAL", "message": "credential store returned an empty reference",
			})
			return
		}
		writeWorkspaceJSON(w, http.StatusOK, map[string]string{"credential_ref": ref})
	})
}
