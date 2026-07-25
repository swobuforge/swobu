// Package canonical owns Swobu's ordered semantic request and response grammar.
//
// A CanonicalItem is an exclusive message, tool call, tool result, or reasoning
// artifact. Reasoning owns ordered portable summary/trace parts plus one
// optional closed opaque-thinking branch. Opaque fields clone defensively,
// format as redacted values, and remain attached to their exact artifact. Messages and
// tool results preserve their distinct, ordered part grammars. Tool calls
// bind a typed correlation ID, immutable ToolKey, and object-or-text input, so
// historical calls remain intelligible without the current ToolSet. The active
// closed ToolDeclaration set owns present-tense callability and schema binding.
// JSONObject owns deterministic object-semantic values, while ToolSet owns
// declaration uniqueness, lookup, and source order. Function and custom tools
// are caller-resolved. Web search is a stateless marker for provider-hosted
// search availability; source-protocol preferences and provider dialects stay
// outside canonical. Observed web-search lifecycles retain their original call
// identity, action, action inputs, result, sources, and citations. They are
// historical facts, not a promise that every destination wire can serialize
// every lifecycle shape. Citation excerpts belong to citation evidence, never
// to returned-source metadata.
//
// Request-band omission is stored with each field through Specified values;
// typed InstructionSet values preserve directive role and order. MessageRole
// names message authorship; TurnOwner is deliberately only user or assistant.
// Session resumption merges ordinary request bands explicitly. Reasoning
// controls and inference effort remain per-invocation except when matching tool
// results continue an unfinished assistant turn under its existing compute.
// RequestPartRef names durable request-tree
// occurrences; ItemPosition is only a progressive stream coordinate. A
// response stream has one response envelope, one finish, at most one usage,
// and either completed success or terminal error. Response construction admits
// only assistant messages, reasoning artifacts, and tool calls; request-only
// tool results cannot become provider output. Completed items remain
// response-projection checkpoints.
//
// # Canonical admission
//
// A fact belongs in the canonical graph only when omitting it can change:
//
//   - client-visible output,
//   - model or provider behavior on this or a later invocation, or
//   - correctness of execution, continuation, correlation, or projection.
//
// Wire presence and byte-equivalent round-trip are not sufficient reasons.
//
// A protocol-specific fact requires a named consumer and a behavioral test.
// Unknown members on known items are ignored. Unknown item kinds are rejected
// unless a concrete continuation requirement justifies a narrowly typed
// representation.
//
// Canonical therefore stores the minimum sufficient state for observable
// projection, inference-equivalent continuation, and correct execution. Typed
// protocol-native values remain only beneath the semantic owner whose named
// consumer needs them, such as encrypted reasoning replay and a target-bound
// continuation handle. Independent protocol transcripts, provider wire names,
// aliases, transport DTOs, and runtime mechanics remain outside this package.
package canonical
