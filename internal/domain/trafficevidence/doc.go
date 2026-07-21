// Package trafficevidence owns immutable traffic facts about request handling.
//
// It defines the traffic-event vocabulary used to describe what happened during
// request execution (including normalized client provenance and model-
// resolution facts such as requested, resolved, resolution mode, and the
// selected provider/model target description) plus
// optional token/cache usage counters, including reasoning-token breakdowns
// when the provider exposes them. Traffic evidence is a projection seam, not a
// behavior authority: it records observed facts so projections can summarize
// them, but it does not decide routing, timing, or control flow.
package trafficevidence
