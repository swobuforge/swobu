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
//
// Image placement is a static protocol-grammar fact. Exchange materializes
// every URL image into validated immutable bytes before provider execution so
// retries and replay observe the same content. MediaFetcher owns bounded network
// I/O only, while InspectImage is the shared pure validator for fetched and
// inline bytes. Codecs receive prepared legal inline carriers and remain pure.
// Attempt-local tool allocation reserves legal literal names before
// assigning salted bounded aliases, so every valid tool set is projectable.
// Protocol-specific response identity stays in typed canonical refinements.
// Provider contracts carry no continuation sidecar; routing-owned target ID
// and version are used only to test refinement applicability.
package provider
