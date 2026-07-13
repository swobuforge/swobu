// Package exchange is the request-path bounded context: ingress, orchestration,
// and execution contract.
//
// This package owns:
//   - Client request ingress (wire decode, endpoint resolution)
//   - Routing orchestration (one machine-driven lifecycle per request)
//   - Provider execution contract (ProviderRequest, RoutableTarget, ExecutionContract)
//   - The codec bridge surface (ClientCodec, ProviderRequestDocumentEncoder, etc.)
//
// It does NOT own:
//   - Routing policy (internal/routing)
//   - Provider adapters (internal/adapters/outbound/providers)
//   - Machine engine (internal/machine)
//
// Sub-packages:
//   - stage: pipeline stage constants
//
// Import rules:
//   - exchange → routing, machine, profile, observation, domain
//   - Nothing may import exchange except adapters and bootstrap.
package exchange
