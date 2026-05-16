package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/swobuforge/swobu/internal/app/operator/authplane"
)

type authSessionStartFunc func(context.Context, authplane.StartInput) (authplane.StartOutput, error)
type authSessionPollFunc func(context.Context, string) (authplane.SessionOutput, error)
type authSessionCancelFunc func(context.Context, string) error
type authSessionRetryFunc func(context.Context, string) (authplane.StartOutput, error)

type chatGPTLoginErrorEnvelope struct {
	Error chatGPTLoginError `json:"error"`
}

type chatGPTLoginError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ChatGPTLoginHandler struct {
	authStart   authSessionStartFunc
	authSession authSessionPollFunc
	authCancel  authSessionCancelFunc
	authRetry   authSessionRetryFunc
}

func NewAuthSessionHandler(
	start authSessionStartFunc,
	session authSessionPollFunc,
	cancel authSessionCancelFunc,
	retry authSessionRetryFunc,
) ChatGPTLoginHandler {
	return ChatGPTLoginHandler{authStart: start, authSession: session, authCancel: cancel, authRetry: retry}
}

func (h ChatGPTLoginHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch {
	case req.Method == http.MethodPost && req.URL.Path == "/_swobu/auth/sessions":
		h.serveGenericStart(w, req)
	case req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/_swobu/auth/sessions/") && strings.HasSuffix(req.URL.Path, "/cancel"):
		h.serveGenericCancel(w, req)
	case req.Method == http.MethodPost && strings.HasPrefix(req.URL.Path, "/_swobu/auth/sessions/") && strings.HasSuffix(req.URL.Path, "/retry"):
		h.serveGenericRetry(w, req)
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/_swobu/auth/sessions/"):
		h.serveGenericSession(w, req)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h ChatGPTLoginHandler) serveGenericStart(w http.ResponseWriter, req *http.Request) {
	if h.authStart == nil {
		writeChatGPTLoginError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "auth session start is unavailable")
		return
	}
	var body struct {
		ProviderSpec string `json:"provider_spec"`
		EndpointRef  string `json:"endpoint_ref"`
		AuthMode     string `json:"auth_mode"`
	}
	_ = json.NewDecoder(req.Body).Decode(&body)
	slog.Debug("auth session start HTTP request",
		"component", "httpapi",
		"provider_spec", strings.TrimSpace(strings.ToLower(body.ProviderSpec)), // trimlowerlint:allow boundary canonicalization
		"has_endpoint_ref", strings.TrimSpace(body.EndpointRef) != "", // trimlowerlint:allow boundary canonicalization
		"auth_mode", strings.TrimSpace(body.AuthMode), // trimlowerlint:allow boundary canonicalization
	)
	out, err := h.authStart(req.Context(), authplane.StartInput{
		ProviderSpec: strings.TrimSpace(body.ProviderSpec), // trimlowerlint:allow boundary canonicalization
		EndpointRef:  strings.TrimSpace(body.EndpointRef),  // trimlowerlint:allow boundary canonicalization
		AuthMode:     strings.TrimSpace(body.AuthMode),     // trimlowerlint:allow boundary canonicalization
	})
	if err != nil {
		slog.Warn("auth session start HTTP failed",
			"component", "httpapi",
			"provider_spec", strings.TrimSpace(strings.ToLower(body.ProviderSpec)), // trimlowerlint:allow boundary canonicalization
			"error", err.Error(),
		)
		writeChatGPTLoginError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
		return
	}
	writeChatGPTLoginJSON(w, http.StatusOK, map[string]any{
		"provider_spec": strings.TrimSpace(strings.ToLower(body.ProviderSpec)), // trimlowerlint:allow boundary canonicalization
		"session_id":    out.SessionID,
		"authorize_url": out.AuthorizeURL,
		"user_code":     out.UserCode,
		"state":         string(out.State),
	})
}

func (h ChatGPTLoginHandler) serveGenericSession(w http.ResponseWriter, req *http.Request) {
	if h.authSession == nil {
		writeChatGPTLoginError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "auth session status is unavailable")
		return
	}
	sessionID := strings.TrimSpace(strings.TrimPrefix(req.URL.Path, "/_swobu/auth/sessions/")) // trimlowerlint:allow boundary canonicalization
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
		if strings.Contains(strings.ToLower(msg), "unknown") { // trimlowerlint:allow boundary canonicalization
			writeChatGPTLoginError(w, http.StatusNotFound, "NOT_FOUND", msg)
			return
		}
		writeChatGPTLoginError(w, http.StatusBadGateway, "UNAVAILABLE", msg)
		return
	}
	writeChatGPTLoginJSON(w, http.StatusOK, map[string]any{
		"provider_spec":  strings.TrimSpace(out.ProviderSpec), // trimlowerlint:allow boundary canonicalization
		"session_id":     out.SessionID,
		"state":          string(out.State),
		"credential_ref": out.CredentialRef,
		"error":          out.ErrorMessage,
	})
}

func (h ChatGPTLoginHandler) serveGenericCancel(w http.ResponseWriter, req *http.Request) {
	if h.authCancel == nil {
		writeChatGPTLoginError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "auth session cancel is unavailable")
		return
	}
	sessionID := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/_swobu/auth/sessions/"), "/cancel")) // trimlowerlint:allow boundary canonicalization
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
		if strings.Contains(strings.ToLower(msg), "unknown") { // trimlowerlint:allow boundary canonicalization
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

func (h ChatGPTLoginHandler) serveGenericRetry(w http.ResponseWriter, req *http.Request) {
	if h.authRetry == nil {
		writeChatGPTLoginError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "auth session retry is unavailable")
		return
	}
	sessionID := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/_swobu/auth/sessions/"), "/retry")) // trimlowerlint:allow boundary canonicalization
	slog.Debug("auth session retry HTTP request",
		"component", "httpapi",
		"session_id", sessionID,
	)
	if sessionID == "" || strings.Contains(sessionID, "/") {
		writeChatGPTLoginError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "auth session id is required")
		return
	}
	out, err := h.authRetry(req.Context(), sessionID)
	if err != nil {
		slog.Warn("auth session retry HTTP failed",
			"component", "httpapi",
			"session_id", sessionID,
			"error", err.Error(),
		)
		msg := err.Error()
		if strings.Contains(strings.ToLower(msg), "unknown") { // trimlowerlint:allow boundary canonicalization
			writeChatGPTLoginError(w, http.StatusNotFound, "NOT_FOUND", msg)
			return
		}
		writeChatGPTLoginError(w, http.StatusBadRequest, "INVALID_ARGUMENT", msg)
		return
	}
	writeChatGPTLoginJSON(w, http.StatusOK, map[string]any{
		"session_id":    out.SessionID,
		"authorize_url": out.AuthorizeURL,
		"user_code":     out.UserCode,
		"state":         string(out.State),
	})
}

func writeChatGPTLoginJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeChatGPTLoginError(w http.ResponseWriter, status int, code string, message string) {
	writeChatGPTLoginJSON(w, status, chatGPTLoginErrorEnvelope{
		Error: chatGPTLoginError{
			Code:    strings.TrimSpace(code),    // trimlowerlint:allow boundary canonicalization
			Message: strings.TrimSpace(message), // trimlowerlint:allow boundary canonicalization
		},
	})
}
