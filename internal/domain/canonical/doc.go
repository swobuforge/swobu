// Package canonical owns Swobu's ordered semantic request and response grammar.
//
// A CanonicalItem is an exclusive scoped message, scoped tool declaration
// contribution, tool call, discovery result, tool result, or
// reasoning artifact. Request-scoped context is legal only in the leading prelude;
// history scope survives request boundaries. ToolEnvironmentAt derives
// availability at one position and rejects conflicting redeclarations.
// Namespaces retain their tree, while discovery results add loaded declarations
// only after their ordered position. Reasoning owns ordered portable
// summary/trace parts plus one
// optional closed opaque-thinking branch. Opaque fields clone defensively,
// format as redacted values, and remain attached to their exact artifact. User
// messages may carry portable images; assistant, system, and developer
// directives remain text-only. Messages and tool results preserve their
// distinct, ordered part grammars. Tool calls
// bind a typed correlation ID, immutable ToolKey, and object-or-text input, so
// historical calls remain intelligible without the current environment.
// JSONObject owns deterministic object-semantic values, while ToolSet owns
// top-level declaration uniqueness and source order. ToolEnvironment is the
// derived global lookup and declaration-ownership validator.
// RewriteToolContributions owns the closed grammar of declaration-bearing
// items so consumers transform contributions without duplicating item traversal.
// Function and custom tools are caller-resolved. An MCP source refines its one ToolNamespace
// authority with endpoint and selection meaning; discovered children remain
// ordinary FunctionTool declarations with no execution field. Canonical never
// decides which runtime executes a callable. Credentials and
// live sessions never enter canonical state. Web
// search is a stateless marker for provider-hosted
// search availability; source-protocol preferences and provider dialects stay
// outside canonical. Observed web-search lifecycles retain their original call
// identity, action, action inputs, result, sources, and citations. They are
// historical facts, not a promise that every destination wire can serialize
// every lifecycle shape. Citation excerpts belong to citation evidence, never
// to returned-source metadata.
//
// Request-control omission is stored with each field through Specified values.
// System and developer MessageItems preserve directive role, order, and scope;
// TurnOwner is deliberately only user or assistant. Session resumption retains
// history and repeats request context only while matching results prove an
// unfinished assistant turn. Reasoning
// controls and inference effort remain per-invocation except when matching tool
// results continue an unfinished assistant turn under its existing compute.
// RequestPartRef names durable request-tree
// occurrences; ItemPosition is only a progressive stream coordinate. A
// fully materialized request consumes each tool result against the preceding
// pending call with the same ID; completed pairs release that ID for later
// unambiguous reuse. Function and custom calls consume content results,
// web-search calls consume typed search results, and discovery pairs must agree
// on both result branch and execution owner. Every response tool call reserves
// its ID through the response. Provider-owned web-search and discovery results
// consume their matching reservation and permit later reuse; function and
// custom calls remain reserved because no response-owned result branch consumes
// them. CompletionClass is the closed lifecycle truth; the provider's original
// stop reason remains opaque projection and diagnostic data. Only
// CompletionCompleted rejects a pending provider-hosted web search or
// provider-executed discovery call; caller-executed function, custom, and
// discovery calls may remain pending for the caller. A duplicate pending
// call or duplicate provider result is contradictory canonical truth. A
// response stream has one response envelope, one finish, at most one usage,
// and either completed success or terminal error. Response construction admits
// only assistant messages, reasoning artifacts, tool calls, provider-executed
// discovery results, and provider-hosted search results; caller-only content
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
