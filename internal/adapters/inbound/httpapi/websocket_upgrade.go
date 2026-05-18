package httpapi

import (
	"net/http"
	"strings"
)

func websocketUpgrade(r *http.Request) bool {
	if r == nil {
		return false
	}
	connection := strings.ToLower(strings.TrimSpace(r.Header.Get("Connection"))) // swobu:io-string source=boundary
	upgrade := strings.ToLower(strings.TrimSpace(r.Header.Get("Upgrade")))       // swobu:io-string source=boundary
	return strings.Contains(connection, "upgrade") && upgrade == "websocket"
}
