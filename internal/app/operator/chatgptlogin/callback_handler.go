package chatgptlogin

import (
	"html"
	"net/http"
	"net/url"
	"strings"

	"log/slog"
)

func (s *LoginService) HandleCallback(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := req.URL.Query()
	state := strings.TrimSpace(q.Get("state"))                                                                                                                                                                    // swobu:io-string source=boundary
	slog.Info("chatgpt auth callback received", "component", "chatgpt_login", "has_state", state != "", "has_code", strings.TrimSpace(q.Get("code")) != "", "has_error", strings.TrimSpace(q.Get("error")) != "") // swobu:io-string source=boundary
	if state == "" {
		http.Error(w, "missing state", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	sid, ok := lookupSessionIDByCallbackState(s.sessionByState, state)
	if !ok {
		slog.Warn("chatgpt auth callback state did not match active session", "component", "chatgpt_login")
		s.mu.Unlock()
		writeAuthenticationErrorPage(w, http.StatusNotFound, callbackRequestID(q))
		return
	}
	rec, ok := s.sessions[sid]
	if !ok {
		s.mu.Unlock()
		writeAuthenticationErrorPage(w, http.StatusNotFound, callbackRequestID(q))
		return
	}
	if rec.terminal {
		slog.Info("chatgpt auth callback ignored because session is terminal", "component", "chatgpt_login", "session_id", sid)
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>Login already completed. You can close this tab.</body></html>"))
		return
	}
	if errValue := strings.TrimSpace(q.Get("error")); errValue != "" { // swobu:io-string source=boundary
		slog.Warn("chatgpt auth callback returned oauth error", "component", "chatgpt_login", "session_id", sid, "oauth_error", errValue)
		message := "oauth error: " + errValue
		if requestID := callbackRequestID(q); requestID != "" {
			message += " (request_id: " + requestID + ")"
		}
		s.setTerminalStateLocked(rec, SessionFailed, message)
		s.mu.Unlock()
		writeAuthenticationErrorPage(w, http.StatusBadRequest, callbackRequestID(q))
		return
	}
	code := strings.TrimSpace(q.Get("code")) // swobu:io-string source=boundary
	if code == "" {
		slog.Warn("chatgpt auth callback missing oauth code", "component", "chatgpt_login", "session_id", sid)
		message := "oauth callback missing code"
		if requestID := callbackRequestID(q); requestID != "" {
			message += " (request_id: " + requestID + ")"
		}
		s.setTerminalStateLocked(rec, SessionFailed, message)
		s.mu.Unlock()
		writeAuthenticationErrorPage(w, http.StatusBadRequest, callbackRequestID(q))
		return
	}
	rec.oauthCode = code
	rec.state = SessionPending
	slog.Info("chatgpt auth callback accepted oauth code", "component", "chatgpt_login", "session_id", sid)
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<html><body>ChatGPT login received. You can return to Swobu.</body></html>"))
}

func callbackRequestID(q url.Values) string {
	if q == nil {
		return ""
	}
	for _, key := range []string{"request_id", "requestId", "x_request_id", "x-request-id"} {
		if value := strings.TrimSpace(q.Get(key)); value != "" { // swobu:io-string source=boundary
			return value
		}
	}
	return ""
}

func lookupSessionIDByCallbackState(sessionByState map[string]string, rawState string) (string, bool) {
	state := strings.TrimSpace(rawState) // swobu:io-string source=boundary
	if state == "" {
		return "", false
	}
	if sid, ok := sessionByState[state]; ok {
		return sid, true
	}
	for _, marker := range []string{"https://", "http://"} {
		if idx := strings.Index(state, marker); idx > 0 {
			if sid, ok := sessionByState[strings.TrimSpace(state[:idx])]; ok { // swobu:io-string source=boundary
				return sid, true
			}
		}
	}
	return "", false
}

func writeAuthenticationErrorPage(w http.ResponseWriter, statusCode int, requestID string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	message := "An error occurred during authentication. Please try again."
	contact := "You can contact us through our help center at help.openai.com if you keep seeing this error."
	if requestID != "" {
		contact += " (Please include the request ID " + requestID + " in your email.)"
	}
	page := "<html><body><h1>Authentication Error</h1><p>" + html.EscapeString(message) + "</p><p>" + html.EscapeString(contact) + "</p><p>Terms of Use Privacy Policy</p></body></html>"
	_, _ = w.Write([]byte(page))
}
