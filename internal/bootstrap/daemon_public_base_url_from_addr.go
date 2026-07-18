package bootstrap

import (
	"net"
	"strings"
)

func daemonPublicBaseURLFromAddr(raw string) string {
	addr := strings.TrimSpace(raw) // swobu:io-string source=boundary
	if addr == "" {
		return "http://127.0.0.1:7926"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:7926"
	}
	return "http://" + net.JoinHostPort(host, port)
}
