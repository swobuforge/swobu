package replay

// Scope partitions replay storage so that IDs from one namespace or caller
// do not leak into another.
type Scope struct {
	// Namespace partitions endpoint/application replay.
	Namespace string
	// CallerKey partitions user/workspace/session replay.
	CallerKey string
}
