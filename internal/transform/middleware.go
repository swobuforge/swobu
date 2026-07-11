package transform

// MiddlewareCapabilities declares whether one semantic transform buffers
// request or response truth, requires replay, or breaks progressive streaming.
type MiddlewareCapabilities struct {
	BuffersRequest  bool
	BuffersResponse bool
	RequiresReplay  bool
	BreaksStreaming bool
}

// BlocksProgressiveStreaming reports whether the transform can force a
// source-incremental response to become stream-shaped batch output.
func (c MiddlewareCapabilities) BlocksProgressiveStreaming() bool {
	return c.BuffersResponse || c.BreaksStreaming
}
