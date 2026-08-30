package bootstrap

import "net/http"

func newDaemonHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: daemonReadHeaderTimeout,
		ReadTimeout:       daemonReadTimeout,
		IdleTimeout:       daemonIdleTimeout,
	}
}
