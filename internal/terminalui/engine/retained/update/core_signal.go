package update

import "github.com/swobuforge/swobu/internal/terminalui/core"

// CoreSignalAction carries one semantic signal emitted by a lowered core node.
// Reducers can translate this into app-specific actions when the bridge is in use.
type CoreSignalAction struct {
	Signal core.Signal
}
