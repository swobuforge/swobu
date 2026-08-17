// Package trafficevidence owns immutable traffic facts about request handling.
//
// It defines the traffic-event vocabulary used to describe what happened during
// request execution, including provider-inflight and terminal lifecycle facts,
// normalized client provenance, model-resolution facts such as requested and
// resolved model identity, optional token/cache usage counters, and
// privacy-safe reusable-prefix evidence. Token usage can include reasoning
// breakdowns when the provider exposes them. Traffic evidence is a projection seam, not a
// behavior authority: it records observed facts so projections can summarize
// them, but it does not decide routing, timing, or control flow.
package trafficevidence
