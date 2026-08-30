// Package exchange is the request-path orchestration bounded context.
//
// It owns:
//   - client ingress orchestration after wire decode;
//   - explicit checkpoint lookup and exact unique current-head history lookup;
//   - session Draft preparation, request-scoped MCP opening, and final freeze;
//   - ordered monotonic candidate execution, cross-exchange exact-target
//     backoff, semantic same-target request-shape recovery, and route failover;
//   - provider-inflight traffic evidence after concrete target selection and
//     immediately before each provider call;
//   - truthful provider-attempt lifecycle logs from command start through
//     provider ingress, client acceptance, and response-completion settlement;
//   - exact-target selection of concrete OpenAI Responses continuation data;
//   - one exchange-scoped read-through image fetch cache reused across attempts;
//   - canonical response capture and atomic session start/head advancement before
//     terminal client publication;
//   - delayed handoff for specifically required provider-hosted effects so an
//     eligible terminal rejection can advance the route without mixing output;
//   - delayed MCP handoff, local tool execution, provider re-entry, usage
//     accumulation, and compatibility evidence.
//
// Provider requests always carry one complete canonical request. Optional
// PreviousHistory metadata authorizes an exact provider codec to omit one
// prior-history range while emitting its typed native continuation handle. A
// target change, target-version change, invalid provider reference, or
// unsupported provider receives the same complete request without metadata.
// Routing connections become complete provider TargetSnapshots at this edge.
// Bedrock endpoint resolution and signing region are supplied atomically during
// that projection; exchange never patches provider facts after construction.
//
// Provider encoding receives an exchange-scoped read-through image resolver
// backed by the existing fetch policy, limits, fetcher, inspection, and cache.
// URL-native codecs preserve locators without invoking it; byte-only codecs
// resolve only the URL images they must lower inline. Fetched bytes never become
// checkpoint or session truth, and resolution is not a reducer phase or durable
// state.
//
// It does not own routing policy, provider adapter semantics, canonical MCP
// meaning, client history fingerprint representation, persistent checkpoints,
// or generic tool-runtime infrastructure.
//
// Import rules:
//   - exchange may import routing, provider, session, profile, observation,
//     canonical domain, wire contracts, and MCP;
//   - only adapters and bootstrap may import exchange.
package exchange
