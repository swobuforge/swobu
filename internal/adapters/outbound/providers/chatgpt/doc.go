// Package chatgpt owns ChatGPT provider adaptation.
//
// Execute owns ChatGPT HTTP-edge execution policy (endpoint and provider
// headers) while reusing responses protocol realization/decoding as stateless
// codec logic. Model catalog loading is provider-owned and sourced from
// opencore bundled tier lists.
package chatgpt
