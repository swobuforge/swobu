// Package canonical owns Swobu's ordered semantic request and response grammar.
//
// A CanonicalItem is an exclusive message, tool call, or tool result. Messages
// and tool results preserve their distinct, ordered part grammars. Tool calls
// bind a typed correlation ID, immutable ToolKey, and object-or-text input, so
// historical calls remain intelligible without the current ToolSet. The active
// closed ToolDeclaration set owns present-tense callability and schema binding.
// JSONObject owns deterministic object-semantic values, while ToolSet owns
// declaration uniqueness, lookup, and source order.
//
// Request-band omission is stored with each field through Specified values;
// typed InstructionSet values preserve directive role and order. MessageRole
// names message authorship; TurnOwner is deliberately only user or assistant.
// Replay merges request bands explicitly and appends ordered items without a
// parallel presence schema. RequestPartRef names durable request-tree
// occurrences; ItemPosition is only a progressive stream coordinate. A
// response stream has one response envelope, one finish, at most one usage,
// and either completed success or terminal error. Response construction admits
// only assistant messages and tool calls; request-only tool results cannot
// become provider output. Completed items remain replay checkpoints.

// Transcript-prefix fingerprints describe prior semantic items without
// retaining quadratic prefix material; candidate verification bytes are
// recomputed on demand. Invocation fingerprints require already-materialized
// requests and include the current ordered request environment. Neither is
// provider cache identity.
//
// Provider wire names, aliases, transport DTOs, and runtime mechanics remain
// outside this package. ResponseRef is the typed exception for response identity
// and exact-target native continuation refinement.
package canonical
