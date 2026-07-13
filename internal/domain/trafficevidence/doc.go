// Package trafficevidence owns immutable traffic facts about request handling.
//
// It defines the traffic-event vocabulary used to describe what happened during
// request execution (including normalized client provenance and model-
// resolution facts such as requested, resolved, and resolution mode) plus
// optional token/cache usage counters, including reasoning-token breakdowns
// when the provider exposes them, without letting transport DTOs, logs, or
// hidden control logic become traffic truth.
package trafficevidence
