package httpapi

import (
	"errors"
	"net/http"
	"net/url"

	connectapp "github.com/swobuforge/swobu/internal/app/operator/connect"
	"github.com/swobuforge/swobu/internal/clientconnect"
	"github.com/swobuforge/swobu/internal/routing"
)

// NewClientConfigurationHandler exposes trusted local Connect review and apply.
// Trusted consumers own human confirmation; request data is not model approval.
// Apply repeats admission, retains original intent, and never retries uncertainty.
func NewClientConfigurationHandler(service *connectapp.Service, attempts *connectapp.Attempts, baseURL string, supports func(*http.Request, routing.WorkspaceSlug, string) bool) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !IsLocalOperatorRequest(request) {
			writeOperatorError(writer, http.StatusForbidden, "INVALID_ARGUMENT", "Local Node admission is required.")
			return
		}
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			writeOperatorError(writer, http.StatusMethodNotAllowed, "INVALID_ARGUMENT", "Configuration preview method is not allowed.")
			return
		}
		query, err := url.ParseQuery(request.URL.RawQuery)
		if err != nil || len(query) != 1 || len(query["dry_run"]) != 1 || (query.Get("dry_run") != "true" && query.Get("dry_run") != "false") {
			writeOperatorError(writer, http.StatusBadRequest, "INVALID_ARGUMENT", "Explicit dry_run=true or false is required.")
			return
		}
		var input struct {
			Workspace string             `json:"workspace"`
			ModelID   string             `json:"model_id"`
			Client    string             `json:"client"`
			Principal string             `json:"principal"`
			Review    *connectapp.Review `json:"review,omitempty"`
		}
		if err := decodeOperatorJSONObject(writer, request, &input, "client configuration preview"); err != nil {
			writeOperatorError(writer, http.StatusBadRequest, "INVALID_ARGUMENT", "Invalid configuration preview.")
			return
		}
		slug, err := routing.ParseWorkspaceSlug(input.Workspace)
		if err != nil || slug.String() != input.Workspace {
			writeOperatorError(writer, http.StatusBadRequest, "INVALID_ARGUMENT", "An exact Workspace is required.")
			return
		}
		if _, err := clientconnect.ParseClientID(input.Client); err != nil {
			writeOperatorError(writer, http.StatusBadRequest, "INVALID_ARGUMENT", "Unsupported client.")
			return
		}
		preview := query.Get("dry_run") == "true"
		if preview && input.Review != nil {
			writeOperatorError(writer, http.StatusBadRequest, "INVALID_ARGUMENT", "Preview does not accept retained review.")
			return
		}
		if input.ModelID == "" || supports == nil || !supports(request, slug, input.ModelID) {
			writeOperatorError(writer, http.StatusBadRequest, "INVALID_ARGUMENT", "Model ID must match an enabled Workspace route.")
			return
		}
		target, err := clientconnect.NewTarget(slug.String(), baseURL+"/workspaces/"+slug.String())
		if err != nil || service == nil {
			writeOperatorError(writer, http.StatusServiceUnavailable, "UNAVAILABLE", "Configuration preview is unavailable.")
			return
		}
		target = target.WithModelID(input.ModelID).WithAuthentication(input.Principal, "")
		writer.Header().Set("Cache-Control", "no-store")
		if !preview {
			if len(request.Header.Values("If-Match")) > 1 || len(request.Header.Values("Idempotency-Key")) > 1 {
				writeOperatorError(writer, http.StatusBadRequest, "INVALID_ARGUMENT", "Apply preconditions must be singular.")
				return
			}
			if input.Review == nil || input.Review.Revision == "" || request.Header.Get("If-Match") == "" {
				writeOperatorError(writer, http.StatusPreconditionRequired, "INVALID_ARGUMENT", "Original review and If-Match are required.")
				return
			}
			if request.Header.Get("If-Match") != `"`+input.Review.Revision+`"` {
				writeOperatorError(writer, http.StatusPreconditionFailed, "INVALID_ARGUMENT", "If-Match does not match original review.")
				return
			}
			key := request.Header.Get("Idempotency-Key")
			if len(key) < 16 || len(key) > 256 || input.Review.ConfigPaths == nil || input.Review.Changes == nil {
				writeOperatorError(writer, http.StatusBadRequest, "INVALID_ARGUMENT", "Exact retained review and retry key are required.")
				return
			}
			result, err := service.ApplyAttempt(request.Context(), attempts, key, input.Client, *input.Review, target, func() error {
				if !IsLocalOperatorRequest(request) || !supports(request, slug, input.ModelID) {
					return errors.New("local Node admission or Workspace is unavailable")
				}
				return nil
			})
			if err != nil {
				var delivery *connectapp.CredentialDeliveryError
				if errors.As(err, &delivery) {
					writeOperatorError(writer, http.StatusConflict, "UNAVAILABLE", delivery.Error())
					return
				}
				status, code, message := http.StatusServiceUnavailable, "UNAVAILABLE", "Configuration outcome is uncertain; inspect without retrying effects."
				switch {
				case errors.Is(err, connectapp.ErrAttemptIntent):
					status, code, message = http.StatusBadRequest, "INVALID_ARGUMENT", "Retained review does not match original intent."
				case errors.Is(err, connectapp.ErrReviewChanged):
					status, code, message = http.StatusPreconditionFailed, "INVALID_ARGUMENT", "Client configuration changed after review."
				case errors.Is(err, connectapp.ErrAttemptConflict):
					status, code, message = http.StatusConflict, "INVALID_ARGUMENT", "Retry key belongs to another configuration intent."
				case errors.Is(err, connectapp.ErrAttemptIndeterminate):
					status, code, message = http.StatusConflict, "UNAVAILABLE", "Original attempt is indeterminate; reconcile without redispatch."
				}
				writeOperatorError(writer, status, code, message)
				return
			}
			writer.Header().Set("ETag", `"`+result.Revision+`"`)
			writeOperatorJSON(writer, http.StatusOK, result)
			return
		}
		plan, err := service.Plan(request.Context(), input.Client, target)
		if err != nil {
			if errors.Is(err, connectapp.ErrModelsPermission) {
				writeOperatorError(writer, http.StatusForbidden, "INVALID_ARGUMENT", "Select an existing Principal with current Models permission for this Workspace, and enable the Models service.")
				return
			}
			writeOperatorError(writer, http.StatusServiceUnavailable, "UNAVAILABLE", "Configuration preview is unavailable; inspect local client configuration.")
			return
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("ETag", `"`+plan.Revision+`"`)
		writeOperatorJSON(writer, http.StatusOK, plan)
	})
}
