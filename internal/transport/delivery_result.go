package transport

// DeliveryResultKind classifies the terminal outcome owned by the inbound
// response-delivery adapter.
type DeliveryResultKind string

const (
	DeliverySucceeded              DeliveryResultKind = "succeeded"
	DeliveryClientCancelled        DeliveryResultKind = "client_cancelled"
	DeliveryClientWriteFailed      DeliveryResultKind = "client_write_failed"
	DeliveryProviderStreamFailed   DeliveryResultKind = "provider_stream_failed"
	DeliveryCheckpointCommitFailed DeliveryResultKind = "checkpoint_commit_failed"
	DeliveryExchangeFailed         DeliveryResultKind = "exchange_failed"
)

// DeliveryResult is the concrete terminal result of consuming and writing one
// client response.
type DeliveryResult struct {
	Kind DeliveryResultKind
	Err  error
}

// ValidDeliveryResultKind reports whether k is a recognized delivery outcome. It
// is the strict membership test the terminal-event constructor uses to reject a
// typed-but-unknown value at the source.
func ValidDeliveryResultKind(k DeliveryResultKind) bool {
	switch k {
	case DeliverySucceeded, DeliveryClientCancelled, DeliveryClientWriteFailed, DeliveryProviderStreamFailed, DeliveryCheckpointCommitFailed, DeliveryExchangeFailed:
		return true
	}
	return false
}
