package bootstrap

import (
	"net"
	"strings"
)

func daemonPublicBaseURLFromBindAddr(bindAddr string) string {
	addr := strings.TrimSpace(bindAddr) // swobu:io-string source=boundary
	if addr == "" {
		return "http://127.0.0.1:7926"
	}
	if strings.HasPrefix(strings.ToLower(addr), "http://") || strings.HasPrefix(strings.ToLower(addr), "https://") { // swobu:io-string source=boundary
		return strings.TrimRight(addr, "/")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://127.0.0.1:7926"
	}
	host = strings.TrimSpace(host) // swobu:io-string source=boundary
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
