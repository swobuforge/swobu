// Package chatgpt owns ChatGPT provider adaptation.
//
// Execute owns ChatGPT HTTP-edge execution policy (endpoint and provider
// headers) while reusing responses protocol realization/decoding as stateless
// codec logic. The SSE-only Codex endpoint may omit Content-Type on successful
// streams; this exact-provider edge normalizes only that absence and rejects
// explicit non-SSE metadata. Model catalog loading is provider-owned and
// sourced from opencore bundled tier lists.
package chatgpt
