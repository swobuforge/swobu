// Package openaifamily owns the shared outbound kernel for providers that use
// OpenAI-family HTTP/protocol surfaces.
//
// It owns base URL, credential application, transport execution, backend error
// origin preservation, and protocol wire realization. Provider-specific
// lowering stays in provider policy modules passed to this kernel.
package openaifamily
