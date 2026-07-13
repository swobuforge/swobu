package trafficevidence

type EventKind string

const (
	EventKindProviderInflight EventKind = "provider_inflight"
	EventKindProviderTerminal EventKind = "provider_terminal"
)

type ClientProtocol string

const ClientProtocolUnknown ClientProtocol = "unknown"

type ClientHandler string

const ClientHandlerUnknown ClientHandler = "unknown"

type ClientFamily string

const ClientFamilyUnknown ClientFamily = "unknown"

type NormalizedOp string

const NormalizedOpUnknown NormalizedOp = "unknown"
