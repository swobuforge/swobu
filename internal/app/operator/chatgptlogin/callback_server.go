package chatgptlogin

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

func (s *LoginService) evictExpiredLocked(now time.Time) {
	for sid, rec := range s.sessions {
		if now.Sub(rec.createdAt) <= authSessionTTL {
			continue
		}
		if !rec.terminal && strings.TrimSpace(rec.oauthState) != "" && s.callbackActiveCount > 0 { // swobu:io-string source=boundary
			s.callbackActiveCount--
		}
		delete(s.sessions, sid)
		delete(s.sessionByState, rec.oauthState)
	}
	if s.callbackActiveCount == 0 {
		s.scheduleCallbackShutdownLocked()
	}
}

func (s *LoginService) ensureCallbackServerStartedLocked() error {
	addr := strings.TrimSpace(s.config.CallbackListenAddr) // swobu:io-string source=boundary
	if addr == "" {
		return nil
	}
	if s.callbackServer != nil && s.callbackListener != nil {
		return nil
	}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, s.HandleCallback)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	var (
		ln  net.Listener
		err error
	)
	for _, candidate := range callbackListenCandidates(addr) {
		server.Addr = candidate
		ln, err = net.Listen("tcp", candidate)
		if err == nil {
			break
		}
	}
	if err != nil {
		return fmt.Errorf("chatgpt login callback listener unavailable at %s: %w (use device auth mode if callback ports are in use)", addr, err)
	}
	s.callbackServer = server
	s.callbackListener = ln
	actualPort := ""
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok && tcp.Port > 0 {
		actualPort = fmt.Sprintf("%d", tcp.Port)
	}
	if strings.TrimSpace(s.config.OAuthRedirectBase) == "" && actualPort != "" { // swobu:io-string source=boundary
		s.config.OAuthRedirectBase = "http://localhost:" + actualPort
	}
	go func() { _ = server.Serve(ln) }()
	return nil
}

func callbackListenCandidates(addr string) []string {
	addr = strings.TrimSpace(addr) // swobu:io-string source=boundary
	if strings.EqualFold(addr, defaultCallbackListenAddr) {
		return []string{defaultCallbackListenAddr, fallbackCallbackListenAddr}
	}
	return []string{addr}
}

func (s *LoginService) setTerminalStateLocked(rec *sessionRecord, state SessionState, message string) {
	wasTerminal := rec.terminal
	rec.state = state
	rec.errorMessage = message
	rec.terminal = true
	if wasTerminal || strings.TrimSpace(rec.oauthState) == "" { // swobu:io-string source=boundary
		return
	}
	if s.callbackActiveCount > 0 {
		s.callbackActiveCount--
	}
	if s.callbackActiveCount == 0 {
		s.scheduleCallbackShutdownLocked()
	}
}

func (s *LoginService) scheduleCallbackShutdownLocked() {
	if s.callbackIdleTimer != nil {
		s.callbackIdleTimer.Stop()
	}
	s.callbackIdleTimer = time.AfterFunc(s.callbackIdleTTL, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.callbackActiveCount > 0 || s.callbackServer == nil {
			return
		}
		_ = s.callbackServer.Close()
		s.callbackServer = nil
		s.callbackListener = nil
		s.callbackIdleTimer = nil
	})
}

func (s *LoginService) closeCallbackServerIfIdleLocked() {
	if s.callbackActiveCount > 0 || s.callbackServer == nil {
		return
	}
	if s.callbackIdleTimer != nil {
		s.callbackIdleTimer.Stop()
		s.callbackIdleTimer = nil
	}
	_ = s.callbackServer.Close()
	s.callbackServer = nil
	s.callbackListener = nil
}
