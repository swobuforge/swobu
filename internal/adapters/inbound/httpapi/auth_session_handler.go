package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/authplane"
)

type authSessionPollFunc func(context.Context, string) (authplane.SessionOutput, error)
type authSessionCancelFunc func(context.Context, string) error

type chatGPTLoginErrorEnvelope struct {
	Error chatGPTLoginErrorPayload `json:"error"`
}

type chatGPTLoginErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ChatGPTLoginHandler struct {
	authSession   authSessionPollFunc
	authCancel    authSessionCancelFunc
	startManager  *authplane.AuthSessionManager
	startAttempts *authplane.StartAttempts
}

// WithStartAttempts binds canonical start to the lifecycle's retained dispatch
// owner and fresh local admission.
func (h ChatGPTLoginHandler) WithStartAttempts(manager *authplane.AuthSessionManager, attempts *authplane.StartAttempts) ChatGPTLoginHandler {
	h.startManager, h.startAttempts = manager, attempts
	return h
}

func NewAuthSessionHandler(
	session authSessionPollFunc,
	cancel authSessionCancelFunc,
) ChatGPTLoginHandler {
	return ChatGPTLoginHandler{authSession: session, authCancel: cancel}
}

func (h ChatGPTLoginHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if strings.HasPrefix(req.URL.Path, "/api/v1/auth-sessions") {
		w.Header().Set("Cache-Control", "no-store")
		if req.URL.Path == "/api/v1/auth-sessions" && req.URL.EscapedPath() == req.URL.Path && req.URL.RawQuery == "" && !req.URL.ForceQuery {
			if !IsLocalOperatorRequest(req) {
				writeChatGPTLoginError(w, 403, "FORBIDDEN", "local Operator admission required")
				return
			}
			if req.Method != http.MethodPost {
				w.Header().Set("Allow", "POST")
				w.WriteHeader(405)
				return
			}
			h.serveGenericStart(w, req)
			return
		}
		id := strings.TrimPrefix(req.URL.Path, "/api/v1/auth-sessions/")
		if req.URL.RawQuery != "" || req.URL.ForceQuery || req.URL.EscapedPath() != req.URL.Path || !validAuthSessionID(id) {
			http.NotFound(w, req)
			return
		}
		if !IsLocalOperatorRequest(req) {
			writeChatGPTLoginError(w, http.StatusForbidden, "FORBIDDEN", "local Operator admission required")
			return
		}
		switch req.Method {
		case http.MethodGet:
			h.serveGenericSession(w, req)
		case http.MethodDelete:
			h.serveGenericCancel(w, req)
		default:
			w.Header().Set("Allow", "GET, DELETE")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
		return
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func validAuthSessionID(id string) bool {
	if id == "" {
		return false
	}
	for _, c := range id {
		if c != '-' && c != '_' && !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') && !(c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

func (h ChatGPTLoginHandler) serveGenericStart(w http.ResponseWriter, req *http.Request) {
	if req.URL.Path != "/api/v1/auth-sessions" || h.startManager == nil || h.startAttempts == nil {
		writeChatGPTLoginError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "auth session start is unavailable")
		return
	}
	var body struct {
		ProviderSpec string `json:"provider_spec"`
		Workspace    string `json:"workspace"`
		Route        string `json:"route"`
		TargetID     string `json:"target_id"`
		DraftSubject string `json:"draft_subject"`
		AuthMode     string `json:"auth_mode"`
	}
	if err := decodeOperatorJSONObject(w, req, &body, "auth session start request"); err != nil {
		writeChatGPTLoginError(w, operatorJSONErrorStatus(err), "INVALID_ARGUMENT", err.Error())
		return
	}
	slog.Debug("auth session start HTTP request",
		"component", "httpapi",
		"provider_spec", strings.TrimSpace(strings.ToLower(body.ProviderSpec)), // swobu:io-string source=boundary
		"has_target", strings.TrimSpace(body.TargetID) != "", // swobu:io-string source=boundary
		"auth_mode", strings.TrimSpace(body.AuthMode), // swobu:io-string source=boundary
	)
	if strings.TrimSpace(body.AuthMode) == "" { // swobu:io-string source=boundary
		writeChatGPTLoginError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "auth_mode is required (browser or device)")
		return
	}
	in := authplane.StartInput{
		ProviderSpec: strings.TrimSpace(body.ProviderSpec), // swobu:io-string source=boundary
		Workspace:    strings.TrimSpace(body.Workspace), Route: strings.TrimSpace(body.Route), TargetID: strings.TrimSpace(body.TargetID), DraftSubject: strings.TrimSpace(body.DraftSubject),
		AuthMode: strings.TrimSpace(body.AuthMode), // swobu:io-string source=boundary
	}
	var out authplane.StartOutput
	var err error
	if len(req.Header.Values("Idempotency-Key")) != 1 {
		writeChatGPTLoginError(w, 400, "INVALID_ARGUMENT", "one original Idempotency-Key required")
		return
	}
	out, err = h.startManager.StartAttempt(req.Context(), h.startAttempts, req.Header.Get("Idempotency-Key"), in, func() error {
		if !IsLocalOperatorRequest(req) {
			return errLocalAdmissionForbidden
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, authplane.ErrStartIndeterminate) || errors.Is(err, authplane.ErrStartConflict) {
			code := "AUTH_START_INDETERMINATE"
			if errors.Is(err, authplane.ErrStartConflict) {
				code = "IDEMPOTENCY_CONFLICT"
			}
			writeChatGPTLoginError(w, 409, code, err.Error())
			return
		}
		if errors.Is(err, errLocalAdmissionForbidden) {
			writeChatGPTLoginError(w, 403, "FORBIDDEN", "local Operator admission required")
			return
		}
		slog.Warn("auth session start HTTP failed",
			"component", "httpapi",
			"provider_spec", strings.TrimSpace(strings.ToLower(body.ProviderSpec)), // swobu:io-string source=boundary
			"error", err.Error(),
		)
		writeChatGPTLoginError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
		return
	}
	w.Header().Set("Location", "/api/v1/auth-sessions/"+out.SessionID)
	writeChatGPTLoginJSON(w, http.StatusCreated, map[string]any{
		"provider_spec": strings.TrimSpace(strings.ToLower(body.ProviderSpec)), // swobu:io-string source=boundary
		"session_id":    out.SessionID,
		"authorize_url": out.AuthorizeURL,
		"user_code":     out.UserCode,
		"expires_at":    strings.TrimSpace(out.ExpiresAt), // swobu:io-string source=boundary
		"state":         string(out.State),
	})
}

func (h ChatGPTLoginHandler) serveGenericSession(w http.ResponseWriter, req *http.Request) {
	if h.authSession == nil {
		writeChatGPTLoginError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "auth session status is unavailable")
		return
	}
	sessionID := strings.TrimPrefix(req.URL.Path, "/api/v1/auth-sessions/")
	slog.Debug("auth session poll HTTP request",
		"component", "httpapi",
		"session_id", sessionID,
	)
	if sessionID == "" || strings.Contains(sessionID, "/") {
		writeChatGPTLoginError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "auth session id is required")
		return
	}
	out, err := h.authSession(req.Context(), sessionID)
	if err != nil {
		slog.Warn("auth session poll HTTP failed",
			"component", "httpapi",
			"session_id", sessionID,
			"error", err.Error(),
		)
		msg := err.Error()
		if strings.Contains(strings.ToLower(msg), "unknown") { // swobu:io-string source=boundary
			writeChatGPTLoginError(w, http.StatusNotFound, "NOT_FOUND", msg)
			return
		}
		writeChatGPTLoginError(w, http.StatusBadGateway, "UNAVAILABLE", msg)
		return
	}
	writeChatGPTLoginJSON(w, http.StatusOK, map[string]any{
		"provider_spec":  strings.TrimSpace(out.ProviderSpec), // swobu:io-string source=boundary
		"session_id":     out.SessionID,
		"state":          string(out.State),
		"credential_ref": out.CredentialRef,
		"error":          out.ErrorMessage,
	})
}

func (h ChatGPTLoginHandler) serveGenericCancel(w http.ResponseWriter, req *http.Request) {
	var body []byte
	var bodyErr error
	if req.Body != nil {
		body, bodyErr = io.ReadAll(io.LimitReader(req.Body, 1))
	}
	if bodyErr != nil || len(body) != 0 {
		writeChatGPTLoginError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "auth session cancellation requires an empty body")
		return
	}
	if !IsLocalOperatorRequest(req) {
		writeChatGPTLoginError(w, http.StatusForbidden, "FORBIDDEN", "local Operator admission required")
		return
	}
	if h.authCancel == nil {
		writeChatGPTLoginError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "auth session cancel is unavailable")
		return
	}
	sessionID := strings.TrimPrefix(req.URL.Path, "/api/v1/auth-sessions/")
	slog.Debug("auth session cancel HTTP request",
		"component", "httpapi",
		"session_id", sessionID,
	)
	if sessionID == "" || strings.Contains(sessionID, "/") {
		writeChatGPTLoginError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "auth session id is required")
		return
	}
	if err := h.authCancel(req.Context(), sessionID); err != nil {
		slog.Warn("auth session cancel HTTP failed",
			"component", "httpapi",
			"session_id", sessionID,
			"error", err.Error(),
		)
		msg := err.Error()
		if strings.Contains(strings.ToLower(msg), "unknown") { // swobu:io-string source=boundary
			writeChatGPTLoginError(w, http.StatusNotFound, "NOT_FOUND", msg)
			return
		}
		writeChatGPTLoginError(w, http.StatusBadRequest, "INVALID_ARGUMENT", msg)
		return
	}
	writeChatGPTLoginJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"state":      string(authplane.SessionStateCanceled),
	})
}

func writeChatGPTLoginJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeChatGPTLoginError(w http.ResponseWriter, status int, code string, message string) {
	writeChatGPTLoginJSON(w, status, chatGPTLoginErrorEnvelope{
		Error: chatGPTLoginErrorPayload{
			Code:    strings.TrimSpace(code),    // swobu:io-string source=boundary
			Message: strings.TrimSpace(message), // swobu:io-string source=boundary
		},
	})
}
