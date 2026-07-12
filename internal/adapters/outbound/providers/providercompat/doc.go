// Package providercompat emits provider-edge compatibility decisions and route
// support gates for request projections that happen after provider selection
// but before transport.
//
// It keeps structured-output, tool-schema strictness, and route feature-gate
// decisions consistent across provider adapters without pulling wire-codec
// details into the exchange layer.
package providercompat
