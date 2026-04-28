// Package runtimeevidence owns immutable runtime facts about request handling.
//
// It defines the traffic-event vocabulary used to describe what happened during
// request execution (including normalized ingress provenance and model-
// resolution facts such as requested, resolved, and resolution mode) plus
// optional token/cache usage counters, without letting transport DTOs, logs, or
// hidden control logic become evidence truth.
package runtimeevidence
