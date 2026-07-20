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
