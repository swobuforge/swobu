package reconcile

// RenderOpKind identifies one renderer-facing reconciliation operation.
type RenderOpKind string

const (
	// RenderOpAppendDurableLine appends one durable transcript line.
	RenderOpAppendDurableLine RenderOpKind = "append_durable_line"
	// RenderOpUpdateEphemeralLine replaces the active ephemeral line.
	RenderOpUpdateEphemeralLine RenderOpKind = "update_ephemeral_line"
	// RenderOpPaintFrame replaces the fullscreen frame contents.
	RenderOpPaintFrame RenderOpKind = "paint_frame"
)

// RenderOpEntry is the reconciler-to-renderer contract.
type RenderOpEntry struct {
	Kind       RenderOpKind
	Text       string
	FrameLines []string
}
