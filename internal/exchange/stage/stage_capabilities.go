package stage

// StageCapabilities declares whether one exchange stage patch or wrapper
// buffers request or response truth, requires replay, or breaks progressive
// streaming.
type StageCapabilities struct {
	BuffersRequest  bool
	BuffersResponse bool
	RequiresReplay  bool
	BreaksStreaming bool
}

// BlocksProgressiveStreaming reports whether the stage can force a
// source-incremental response to become stream-shaped batch output.
func (c StageCapabilities) BlocksProgressiveStreaming() bool {
	return c.BuffersResponse || c.BreaksStreaming
}
