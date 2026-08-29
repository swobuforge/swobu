// Package chatgpt owns ChatGPT provider adaptation.
//
// Execute owns ChatGPT HTTP-edge execution policy (endpoint and provider
// headers) while reusing responses protocol realization/decoding as stateless
// codec logic. The Codex request grammar omits canonical max-output limits
// because its Responses endpoint rejects max_output_tokens; the omission is
// emitted as compatibility evidence. The SSE-only endpoint may omit
// Content-Type on successful streams; this exact-provider edge normalizes only
// that absence and rejects explicit non-SSE metadata. Model catalog loading is
// provider-owned and sourced from opencore bundled tier lists.
package chatgpt
