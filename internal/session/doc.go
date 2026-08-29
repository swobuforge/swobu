// Package session owns immutable canonical checkpoints and linear client-session
// heads used to resume one logical model session across independent requests.
//
// Each successfully client-encoded completed response commits one self-contained
// checkpoint partitioned by workspace. A checkpoint retains one complete
// effective canonical request, its canonical response, a mandatory immutable
// codec scheme, an optional visible-history fingerprint for implicit lookup,
// and the internal session lineage it advances. A current head without a
// fingerprint remains explicitly resumable and unindexed. Only current heads
// with fingerprints participate in implicit lookup; older retained checkpoints
// remain available only by explicit Swobu response ID. Head advancement is
// atomic compare-and-swap and rejects codec-scheme changes, so one lineage
// cannot silently branch or change client history grammar.
//
// Resolution receives either one complete genesis request or one codec-rebased
// current invocation plus an already selected checkpoint. It never receives a
// full replay witness together with the current invocation and never compares
// canonical prefixes. Resume restores opaque checkpoint history and unfinished
// tool-turn context, while preserving current invocation controls, and records the exact complete-request item range that an
// applicable OpenAI Responses previous_response_id may replace. The resolved
// request remains complete and immutable; provider lowering data is derived
// separately for the exact target generation that produced it.
//
// Request-scoped MCP preparation occurs through a transient Draft before the
// resolved request is frozen. Local MCP rounds append one explicit complete
// history segment and clear prior provider continuation state.
//
// Checkpoints are a bounded process-local recent window. Expiry, reclamation,
// and daemon restart forget them. Fetched media bytes are execution artifacts
// and never enter session, checkpoint, or history-fingerprint state.
//
// Package session does not own client protocol DTOs, fingerprint composition,
// routing, provider selection, image fetching, response-ID allocation,
// authentication, transport encoding, or client stream lifecycle.
package session
