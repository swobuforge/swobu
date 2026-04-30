package daemonstate

const (
	HeaderReady        = "ready"
	HeaderOfflineStale = "offline (stale)"

	DaemonStateUp            = "up"
	DaemonStateUninitialized = "uninitialized"
	DaemonStateUnreachable   = "unreachable"
	DaemonStateIncompatible  = "incompatible"
)
