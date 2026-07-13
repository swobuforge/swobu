// Package completions maps canonical prompt semantics to and from the
// completions wire protocol. Explicit structured-output requests fail closed
// because this family does not expose an exact structured-output field.
// Tool-bearing requests and explicit at-most-one batch semantics also fail
// closed because this family does not expose a tool surface.
package completions
