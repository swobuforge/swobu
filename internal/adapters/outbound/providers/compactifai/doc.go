// Package compactifai composes CompactifAI's documented model-catalog and
// readable Chat reasoning carriers around the shared OpenAI-family runtime.
// Its Responses stream decoration retains the first item identity observed at
// each output index when CompactifAI rewrites redundant IDs in the terminal
// response snapshot; the shared Responses decoder remains the lifecycle owner.
package compactifai
