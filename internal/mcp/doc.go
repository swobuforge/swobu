// Package mcp owns Swobu's client-executed, request-scoped URL MCP lifecycle.
//
// Access contains transient ingress credentials. Open resolves sources once and
// returns exact canonical history plus a fully initialized Run. Run owns
// official-SDK sessions, bounded per-source attempt declarations, an immutable
// availability transformation, private callable bindings, effect budgets,
// post-effect result projection, dependency-error translation, and close.
// Required sources resolve before optional sources under one total discovery
// deadline. Calls is pure classification; BeginBatch reserves effects only at
// the exchange command boundary. Canonical owns only durable namespace meaning
// and declaration ownership; exchange owns only orchestration consequences.
//
// Responses admission currently requires explicit require_approval:"never".
// Authorization and Authorization: Bearer ingress become transient Access and
// never enter canonical history. Object and array allowed-tool selections both
// become the same canonical selection.
//
// This package does not implement provider-executed Responses mcp_call
// lifecycles, managed connectors, tunnels, approval workflows, arbitrary
// headers, caller restrictions, or deferred loading. Those known semantics
// reject explicitly at the wire boundary; they are never erased into
// ordinary-tool success.
package mcp
