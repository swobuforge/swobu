package bootstrap

import (
	"net/http"
	"time"
)

var providerResponseHeaderTimeout = 5 * time.Minute

func newProviderHTTPClient() *http.Client {
	baseTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{}
	}
	transport := baseTransport.Clone()
	transport.ResponseHeaderTimeout = providerResponseHeaderTimeout
	return &http.Client{Transport: transport}
}
