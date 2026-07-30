// Package provider defines the inward contract between exchange orchestration
// and exact outbound provider backends.
//
// Exchange owns attempts, rounds, routing, fallback, session resolution, and control state.
// Provider values contain only the target and canonical/wire facts required to
// encode, send, and decode one provider request. Concrete implementations live
// under internal/adapters/outbound/providers and must not import exchange.
// Provider adapters also own factual failure classification: unsupported,
// unavailable, rejected, invalid request, cancelled, or internal. Exchange may
// apply recovery policy to those types only after combining them with the
// adapter-owned execution possibility and the final attempt's replay safety.
// Availability alone is never replay permission. Untyped issued-call errors
// conservatively mean provider execution may have occurred.
//
// Exchange materializes every URL image into validated immutable bytes before
// provider execution so retries and full-history execution observe the same
// content. MediaFetcher owns bounded network I/O only, while InspectImage is the
// shared pure validator for fetched and inline bytes. Codecs receive prepared
// legal inline carriers, remain pure, and solely own exact target-grammar
// projection. Successful projection returns non-exact semantic changes as
// values; target-local incompatibility remains a typed error for exchange.
// Attempt-local tool allocation reserves legal literal names before
// assigning salted bounded aliases, so every valid tool set is projectable.
// Protocol-specific response identity stays in typed canonical native handles.
// Session resolution materializes unfinished tool-turn compute continuity into
// the effective canonical request before this boundary. Routing-owned target
// ID and version bind provider-owned native response handles. Self-contained
// opaque thinking remains attached to its canonical reasoning item and replays
// unchanged through the Messages protocol or exact OpenRouter provider hook.
package provider
