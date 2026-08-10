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
// Exchange passes an exchange-scoped read-through image resolver into provider
// encoding. URL-native codecs preserve locators without invoking it; byte-only
// codecs resolve through the existing bounded fetch policy, inspection, and
// cache. Fetched bytes never enter canonical history or checkpoints. Codecs
// solely own exact target-grammar projection. Successful projection returns non-exact semantic changes as
// values; target-local incompatibility remains a typed error for exchange.
// TargetSupport is immutable knowledge for one exact attempt. It answers only
// whether the target can honor canonical meaning; codecs continue to own how
// meaning is represented, and exchange continues to own recovery policy.
// Provider/protocol identity, encoder availability, model names, backend prose,
// and choosing a portable projection do not establish support. Provider runtime
// facets resolve exact-target evidence independently from backend construction.
// AttemptToolNames owns one immutable canonical-key to wire-name bijection for
// an attempt. It preserves safe request-level literals and gives every generated
// alias a readable semantic prefix plus a stable digest of the complete canonical
// identity. Generic naming never infers transport provenance from namespace
// text; typed transformations remove transport-only structure before this edge.
// Protocol-specific response identity stays in typed canonical native handles.
// Session resolution materializes unfinished tool-turn compute continuity into
// the effective canonical request before this boundary. Routing-owned target
// ID and version bind provider-owned native response handles. Self-contained
// opaque thinking remains attached to its canonical reasoning item and replays
// unchanged through the Messages protocol or exact OpenRouter provider hook.
// TargetSnapshot is an atomic execution projection. Provider-specific execution
// facts occupy one closed options arm fixed by construction; adapters read them
// through accessors and cannot complete snapshots through later mutation.
package provider
