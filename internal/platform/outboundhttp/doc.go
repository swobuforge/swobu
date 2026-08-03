// Package outboundhttp owns production net/http network transport construction.
// All Swobu HTTP(S) transports use Go's request-level environment proxy
// selection. Callers may supply a guarded dialer for direct destinations, but
// callers do not parse proxy configuration or classify dial addresses.
package outboundhttp
