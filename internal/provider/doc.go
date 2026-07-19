// Package provider defines the inward contract between exchange orchestration
// and exact outbound provider backends.
//
// Exchange owns attempts, rounds, routing, fallback, replay, and control state.
// Provider values contain only the target and canonical/wire facts required to
// encode, send, and decode one provider request. Concrete implementations live
// under internal/adapters/outbound/providers and must not import exchange.
// Provider adapters also own factual failure classification: unsupported,
// unavailable, rejected, invalid request, cancelled, or internal. Exchange may
// apply fallback policy to those types but must not infer availability from an
// unknown HTTP status or compatibility decision.
// Native continuation identity is projected from routing as target ID and
// version. Adapters may opt into an exact continuation wire contract but never
// derive another identity from provider configuration.
package provider
