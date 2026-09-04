// Package thread owns Swobu's opaque identity for one growing logical client
// conversation. An ID preserves equality across continuity, routing locality,
// and provider-scoped projections without exposing foreign identifier bytes or
// becoming a checkpoint/state selector.
package thread
