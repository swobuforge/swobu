package bootstrap

import (
	"net/http"
	"time"

	"github.com/swobuforge/swobu/internal/platform/outboundhttp"
)

var providerResponseHeaderTimeout = 5 * time.Minute

func newProviderHTTPClient() *http.Client {
	transport := outboundhttp.NewTransport(outboundhttp.Config{ResponseHeaderTimeout: providerResponseHeaderTimeout})
	return &http.Client{Transport: transport}
}
