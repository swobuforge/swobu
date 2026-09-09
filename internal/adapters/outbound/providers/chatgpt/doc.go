// Package chatgpt owns ChatGPT provider adaptation.
//
// Execute owns ChatGPT HTTP-edge execution policy (endpoint and provider
// headers) while reusing responses protocol realization/decoding as stateless
// codec logic. The Codex request grammar omits canonical max-output limits
// because its Responses endpoint rejects max_output_tokens; the omission is
// emitted as compatibility evidence. The SSE-only endpoint may omit
// Content-Type on successful streams; this exact-provider edge normalizes only
// that absence and rejects explicit non-SSE metadata. Authenticated Codex model
// discovery is provider-owned, advisory, and has no bundled entitlement
// fallback. Stored subscription credentials resolve bearer and account-routing
// claims from one snapshot; refresh preserves the last routing-bearing ID token
// when a replacement token omits workspace routing claims.
package chatgpt
