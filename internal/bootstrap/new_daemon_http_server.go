package bootstrap

import "net/http"

func newDaemonHTTPServer(bindAddr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              bindAddr,
		Handler:           handler,
		ReadHeaderTimeout: daemonReadHeaderTimeout,
		ReadTimeout:       daemonReadTimeout,
		WriteTimeout:      daemonWriteTimeout,
		IdleTimeout:       daemonIdleTimeout,
	}
}
