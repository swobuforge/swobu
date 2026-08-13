// Package gemini owns the private native Gemini Interactions adapter. Its
// closed tool grammar lowers ordinary functions and Google Search only. MCP
// sources are resolved and executed by Exchange before this adapter runs; the
// adapter never acquires MCP credentials or execution authority.
//
// Credential-reference presence is the sole authentication selector. A
// non-empty reference resolves to x-goog-api-key; an empty reference consumes
// Google Application Default Credentials and sends Bearer authentication plus
// an available quota project. Detection, token refresh, and safe failure copy
// remain private to this adapter, and neither path falls back to the other.
// This is analogous to Bedrock only at the ambient-versus-reference authoring
// boundary; Google ADC/OAuth and AWS credential-chain/SigV4 mechanics must not
// be merged behind a shared runtime authenticator.
//
// Decode classifies provider semantics in the repository-wide order
// Exact -> Approx -> Drop locally -> Fail. Execution-bearing lifecycle,
// correlation, and replay-required history remain fail-closed. Independent
// additive output with no readable canonical semantic may instead be omitted
// with occurrence-local compatibility evidence when the residual response is
// still valid.
//
// A signature-only thought does not become fabricated readable reasoning. Its
// exact native thought step is retained on OpaqueThinking and can be replayed
// without a native continuation handle. ResponseRef.Interactions and
// previous_interaction_id are optional compression over that canonical truth,
// never the only source of supported continuation.
//
// The Interactions request grammar never becomes a Swobu client protocol or
// shared wire family unless a second provider becomes a demonstrated reader.
package gemini
