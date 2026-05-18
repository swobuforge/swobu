package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

func requestIDFromRequest(r *http.Request) string {
	requestID := strings.TrimSpace(r.Header.Get("X-Request-Id")) // swobu:io-string source=boundary
	if requestID == "" {
		requestID = strings.TrimSpace(r.Header.Get("X-Request-ID")) // swobu:io-string source=boundary
	}
	if requestID == "" {
		requestID = newRequestID()
	}
	return requestID
}

func newRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "swobu-request"
	}
	return hex.EncodeToString(raw[:])
}
