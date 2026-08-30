package bootstrap

import (
	"net/http"

	"github.com/swobuforge/swobu/internal/platform/outboundhttp"
)

func newProviderHTTPClient() *http.Client {
	transport := outboundhttp.NewTransport(outboundhttp.Config{})
	return &http.Client{Transport: transport}
}
